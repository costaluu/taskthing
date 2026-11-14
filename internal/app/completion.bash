# taskthing bash completion. `taskthing completion install` sets it up; by
# hand, add `eval "$(taskthing completion bash)"` to ~/.bashrc.
_taskthing() {
  # bash splits words at : and = (COMP_WORDBREAKS); rejoin the ones typed
  # without a space between, so `b:[…` and `--opt=…` arrive whole.
  local i word rest line=$COMP_LINE words=()
  for ((i = 0; i <= COMP_CWORD; i++)); do
    word=${COMP_WORDS[i]}
    rest=${line#"${line%%[![:space:]]*}"}
    if ((i > 0)) && [[ $rest == "$line" ]]; then
      words[${#words[@]}-1]+=$word
    else
      words+=("$word")
    fi
    line=${rest#"$word"}
  done
  # readline replaces only what follows the last : or =, so the candidates
  # lose what precedes it.
  local cur=${words[${#words[@]}-1]} tail=${COMP_WORDS[COMP_CWORD]}
  [[ $tail == [=:] ]] && tail=
  local prefix=${cur%"$tail"}
  # Inside an open quote readline keeps the quote, so a candidate goes in as
  # it is; outside, escaped.
  # ponytail: a value holding the quote itself isn't escaped; names rarely do.
  local quoted=
  [[ $cur == [\"\']* ]] && quoted=1

  # The words as typed, unquoted: `buy\ m` and `'my board` mean their text.
  # ponytail: drops every backslash and an outer quote; enough for names and titles.
  words=("${words[@]//\\/}")
  words=("${words[@]#[\"\']}")
  words=("${words[@]%[\"\']}")
  local typed=${words[${#words[@]}-1]#"$prefix"}

  local v rest desc display lcp nospace= displayed= values=() escaped=() shown=()
  while IFS= read -r line; do
    # value, description, and what to list the value as when not itself
    v=${line%%$'\t'*} rest= desc= display=
    [[ $line == *$'\t'* ]] && rest=${line#*$'\t'}
    desc=${rest%%$'\t'*}
    [[ $rest == *$'\t'* ]] && display=${rest#*$'\t'}
    if [[ -z $v ]]; then
      # a directive: nothing goes in after the match, not even a closing quote
      [[ $desc == nospace ]] && nospace=1
      continue
    fi
    v=${v#"$prefix"}
    values+=("$v")
    # Escaped in the listing too: readline inserts what the entries share.
    [[ $quoted ]] || printf -v v '%q' "$v"
    escaped+=("$v")
    [[ $display ]] && displayed=1
    if [[ $desc ]]; then shown+=("${display:-$v}  ($desc)"); else shown+=("${display:-$v}"); fi
  done < <(command taskthing __complete "${words[@]:1}" 2>/dev/null)

  COMPREPLY=()
  [[ $nospace ]] && compopt -o nospace
  # Behind a lone match readline closes an open quote, nospace or not. Two
  # entries that share the match put it in and close nothing.
  # ponytail: with show-all-if-ambiguous set, both entries get listed once.
  local open=
  [[ $nospace && $quoted ]] && open=1
  if ((${#values[@]} == 1)); then
    COMPREPLY=("${escaped[0]}")
    [[ $open ]] && COMPREPLY+=("${escaped[0]} ")
  elif ((${#values[@]} > 1)); then
    if [[ $displayed ]]; then
      # Listed by their endings, the entries no longer share what the values
      # do: put that in first, and list once nothing more is shared.
      lcp=${escaped[0]}
      for v in "${escaped[@]}"; do
        while [[ $v != "$lcp"* ]]; do lcp=${lcp%?}; done
      done
      if [[ ${#lcp} -gt ${#typed} && $lcp == "$typed"* ]]; then
        compopt -o nospace
        COMPREPLY=("$lcp")
        [[ $open ]] && COMPREPLY+=("$lcp ")
        return 0
      fi
      COMPREPLY=("${shown[@]}" " ")
      return 0
    fi
    COMPREPLY=("${shown[@]}")
    # Typed a title, got numbers: an entry no number starts like keeps
    # readline from replacing the typed word with the numbers' common prefix.
    for v in "${values[@]}"; do [[ $v == "$typed"* ]] && return 0; done
    COMPREPLY+=(" ")
  fi
  return 0
}
# nosort: numbers and dates come in the order they mean.
complete -o nosort -F _taskthing taskthing
