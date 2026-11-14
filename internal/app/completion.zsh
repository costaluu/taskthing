#compdef taskthing
# taskthing zsh completion. `taskthing completion install` sets it up; by
# hand, add `eval "$(taskthing completion zsh)"` to ~/.zshrc.
_taskthing() {
  local -a values shown opts parts
  local line v cur=${(Q)PREFIX}
  for line in "${(@f)$(command taskthing __complete "${(@Q)words[2,CURRENT-1]}" "$cur" 2>/dev/null)}"; do
    [[ -n $line ]] || continue
    # value, description, and what to list the value as when not itself
    parts=("${(@ps:\t:)line}")
    if [[ -z $parts[1] ]]; then
      # a directive: nothing goes in after the match, not even a closing quote
      [[ $parts[2] == nospace ]] && opts+=(-S '')
      continue
    fi
    values+=("$parts[1]")
    if [[ -n $parts[2] ]]; then
      shown+=("${parts[3]:-$parts[1]}  -- $parts[2]") opts+=(-l)
    else
      shown+=("${parts[3]:-$parts[1]}")
    fi
  done
  (( $#values )) || return 1
  # -U: the binary already matched, by title too, so zsh mustn't re-filter.
  # -V: nor re-order — numbers and dates come in the order they mean.
  compadd -U -V taskthing "${opts[@]}" -d shown -- "${values[@]}"
  # Typed a title, got numbers: list them and keep the word, rather than
  # replace it with the numbers' common prefix.
  for v in $values; do [[ $v == "$cur"* ]] && return 0; done
  (( $#values > 1 )) && compstate[insert]=''
  return 0
}
if [[ $funcstack[1] == _taskthing ]]; then
  _taskthing "$@"
else
  # A .zshrc without compinit has no compdef yet.
  (( $+functions[compdef] )) || { autoload -U compinit && compinit; }
  compdef _taskthing taskthing
fi
