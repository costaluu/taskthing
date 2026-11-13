#compdef taskthing
# taskthing zsh completion. `taskthing completion install` sets it up; by
# hand, add `eval "$(taskthing completion zsh)"` to ~/.zshrc.
_taskthing() {
  local -a values shown opts
  local line v cur=${(Q)PREFIX}
  for line in "${(@f)$(command taskthing __complete "${(@Q)words[2,CURRENT-1]}" "$cur" 2>/dev/null)}"; do
    [[ -n $line ]] || continue
    values+=("${line%%$'\t'*}")
    if [[ $line == *$'\t'* ]]; then
      shown+=("${line%%$'\t'*}  -- ${line#*$'\t'}") opts=(-l)
    else
      shown+=("$line")
    fi
  done
  (( $#values )) || return 1
  # -U: the binary already matched, by title too, so zsh mustn't re-filter.
  compadd -U $opts -d shown -- "${values[@]}"
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
