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

  local v values=() escaped=() shown=()
  while IFS= read -r line; do
    v=${line%%$'\t'*}
    v=${v#"$prefix"}
    values+=("$v")
    # Escaped in the listing too: readline inserts what the entries share.
    [[ $quoted ]] || printf -v v '%q' "$v"
    escaped+=("$v")
    if [[ $line == *$'\t'* ]]; then shown+=("$v  (${line#*$'\t'})"); else shown+=("$v"); fi
  done < <(command taskthing __complete "${words[@]:1}" 2>/dev/null)

  COMPREPLY=()
  if ((${#values[@]} == 1)); then
    COMPREPLY=("${escaped[0]}")
  elif ((${#values[@]} > 1)); then
    COMPREPLY=("${shown[@]}")
    # Typed a title, got numbers: an entry no number starts like keeps
    # readline from replacing the typed word with the numbers' common prefix.
    for v in "${values[@]}"; do [[ $v == "$typed"* ]] && return 0; done
    COMPREPLY+=(" ")
  fi
  return 0
}
complete -F _taskthing taskthing
