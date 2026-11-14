# Estudo: autocomplete por shell

Início do estudo, 2026-11-14. Objetivo declarado: ter o melhor autocomplete por shell do mercado. Este documento registra o que existe hoje, o que foi medido, o que os shells e as bibliotecas de referência oferecem, e uma ordem de trabalho proposta. Nada aqui está decidido nem implementado.

Cada afirmação sobre um shell ou biblioteca cita a fonte primária consultada. O que não foi conferido na fonte está marcado como "a confirmar".

## 1. Como funciona hoje

- O binário decide tudo. `taskthing __complete <palavras…> <atual>` imprime um candidato por linha, `valor\tdescrição` (`internal/app/complete.go`). Ele lê a mesma árvore de comandos, o mesmo corte de flags globais e o mesmo parser que o `Run` usa, então comando novo ganha completion sem código extra (confirmado com `undo`, `today`, `upcoming`, `overdue` e `week`).
- Os shims (`completion.bash`, 58 linhas, e `completion.zsh`, 31 linhas) só escapam, exibem e preservam a palavra digitada. São carregados por `eval "$(taskthing completion <shell>)"`, então acompanham o binário nas atualizações.
- Cobertura atual: comandos e subcomandos com descrição, flags longas, valores de flag (`--opt valor` e `--opt=valor`), boards, workspaces, chaves e valores de config, cores, `--peer`, `--branch`, a tag `b:[…` do `add` e tarefas pelo número da última listagem, encontradas também por trecho do título.
- Shells suportados: bash e zsh.

## 2. Medições

Máquina: i7-1355U, container `agent-dev` (Fedora 44), Go 1.26.9, bash 5.3.9, zsh 5.9. Binário de 12,2 MB. Média de 30 a 100 execuções por linha; workspace local com N tarefas datadas e 2 boards. Piso de `fork+exec` de `/bin/true` na mesma máquina: 0,50 ms.

| Caso | 10 tarefas | 100 tarefas | 1000 tarefas |
| --- | --- | --- | --- |
| `__complete che` (comando) | 2,7 ms | 2,8 ms | 2,7 ms |
| `__complete check ""` (todas as tarefas) | 3,7 ms | 9,1 ms | 50,7 ms |
| `__complete check milk` (por título) | 3,7 ms | 9,1 ms | 51,3 ms |
| `__complete set board 1 ""` | 3,0 ms | 2,9 ms | 2,8 ms |
| `__complete use ""` | 2,8 ms | 2,7 ms | 2,7 ms |
| `__complete truncate --branch ""` (chama `git config`) | 4,9 ms | 4,7 ms | 5,0 ms |
| shim bash completo, `check <TAB>` | não medido | não medido | 68,9 ms |

Leitura:

- O custo fixo é de cerca de 2,7 ms, dos quais 0,5 ms são o piso do sistema. Não é gargalo.
- Completar tarefas cresce linearmente, cerca de 48 µs por tarefa numerada: `taskRefs` abre, faz o parse e valida um arquivo `.md` por número da kv store, mesmo quando o prefixo digitado já excluiria quase todos.
- O shim bash acrescenta cerca de 18 ms para 1000 candidatos (laço `while read` com `printf %q` por linha).
- Abaixo de 100 ms tudo parece instantâneo; o pior caso medido está dentro disso. O risco é o crescimento linear em workspaces grandes e em discos lentos (o cache de página estava quente em todas as medições).
- O único caso que pode passar de 1 s é `--peer`: faz `git ls-remote` na rede, limitado a 3 s por timeout (`peerValue`).

Achado fora do escopo: `list` com 1000 tarefas levou 686 ms (19,8 ms com 100). O crescimento é quadrático, porque `kvStore.set` relê e regrava o JSON inteiro a cada tarefa numerada. Vale um item próprio.

## 3. Lacunas observadas

Conferidas rodando o binário desta árvore:

1. **Ordem dos números no bash.** O binário devolve `1 2 … 12`, mas o shim registra `complete -F` sem `-o nosort`, e o readline ordena alfabeticamente: `1 10 11 12 2 …`. O manual do bash 5.3 define `nosort` como "Tell readline not to sort the list of possible completions alphabetically". No zsh, `compadd` sem `-V`/`-o nosort` também ordena (ver seção 4); o efeito exato sobre números está a confirmar em teste.
2. **Sem listagem, sem candidatos.** Antes do primeiro `list`, `check <TAB>` não oferece nada, porque só existem os números da kv store. Buscar por título não funciona nesse estado.
3. **Datas e recorrência sem ajuda.** `add x d:[to<TAB>`, `set date-time 1 <TAB>` e `list --period <TAB>` não oferecem nada. São justamente os campos em que o usuário precisa lembrar a gramática.
4. **Flags curtas e globais.** `list -h<TAB>` não oferece nada; `--format`, `--version` e as demais globais não aparecem em `list -<TAB>`.
5. **Descrições no bash só quando há mais de um candidato**, no formato `valor  (descrição)`, e sem agrupamento em nenhum shell: comandos, tarefas e flags saem numa lista única.
6. **Busca por título é substring exata** (sem tolerância a erro de digitação nem a palavras fora de ordem), e só sobre a descrição.
7. **Shells ausentes:** fish, PowerShell (o binário é distribuído para Windows), nushell.
8. **Teste do zsh frágil.** `TestZshCompletion` falhava nos containers `dev` e `agent-dev`: com os rc globais do Fedora (`/etc/zshrc`), o que o teste digita antes do prompt se perde. Passa com `zsh -d -i` (sem rc globais), ajuste já aplicado ao teste. Usuários reais não são afetados, mas a suíte não cobria o zsh neste ambiente.

## 4. O que os shells permitem

| Capacidade | bash | zsh | fish | PowerShell | nushell |
| --- | --- | --- | --- | --- | --- |
| Descrição por candidato | Não nativo; o shim precisa desenhar | Sim, `compadd -d` e `-l` | Sim, texto após TAB | Sim, `listItemText` e `toolTip` | Sim, coluna `description` |
| Manter a ordem do binário | `-o nosort` | `-V grupo` ou `-o nosort` | `complete -k` | A confirmar | A confirmar |
| Grupos com título | Não | Sim, `-J`/`-V` e `-X` | Não | Parcial, `resultType` | Não |
| Sufixo que some ao digitar | Só `-o nospace` | Sim, `-S` com `-q`/`-r` | A confirmar | Não | Não |
| Filtrar no binário sem o shell refiltrar | Sim (COMPREPLY é final) | Sim, `-U` | A confirmar | Sim | Sim |

Fontes:

- bash: `man bash` 5.3 (seção "Programmable Completion"), opções `nosort`, `nospace`, `noquote`, `fullquote` e a variável `COMP_TYPE`, que diz ao completer se foi TAB, listagem (`?`) ou menu (`%`).
- zsh: [Completion Widgets](https://zsh.sourceforge.io/Doc/Release/Completion-Widgets.html). `-V` é "identical to the combination of `-J` and `-o nosort`"; `-U` é "normally used in functions that do the matching themselves"; `-q` remove o sufixo de `-S` conforme o próximo caractere digitado; `compstate[insert]` e `compstate[list]` controlam inserção e listagem.
- fish: [Writing your own completions](https://fishshell.com/docs/current/completions.html) e [`complete`](https://fishshell.com/docs/current/cmds/complete.html). Cada linha da substituição de comando é um candidato e "each argument may optionally have a tab character followed by the argument description", exatamente o formato que `__complete` já imprime. `-k` "Keeps the order of ARGUMENTS instead of sorting alphabetically". Arquivos de vendor vão em `vendor_completions.d`.
- PowerShell: [`Register-ArgumentCompleter`](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/register-argumentcompleter). Com `-Native`, o bloco recebe `$wordToComplete`, `$commandAst` e `$cursorPosition`, e devolve `CompletionResult` com `completionText`, `listItemText`, `resultType` e `toolTip`.
- nushell: [External completers](https://www.nushell.sh/cookbook/external_completers.html). Um closure em `$env.config.completions.external.completer` recebe as palavras e devolve linhas com `value` e `description`; a página mostra a saída do fish, em TSV, sendo convertida direto.

## 5. Referências de mercado

- **cobra** ([Shell completions](https://github.com/spf13/cobra/blob/main/site/content/completions/_index.md)): mesma arquitetura que a nossa, um comando oculto `__complete`. A diferença é a última linha da saída, uma diretiva (`:4`) que diz ao shim como se comportar: `ShellCompDirectiveNoSpace`, `NoFileComp`, `KeepOrder`, `FilterFileExt`, `FilterDirs` e `Error`. Suporta bash, zsh, fish e PowerShell. No bash, descrições exigem o script V2.
- **carapace** ([documentação](https://carapace-sh.github.io/carapace/)): gerador de completion para cobra com suporte declarado a Bash, Elvish, Fish, Nushell, Oil, Powershell, Xonsh e Zsh. Recursos como cache, estilos e completion em várias partes estão a confirmar: a página consultada não os descreve.
- A confirmar em próxima rodada: clap (`clap_complete`, modo dinâmico), argcomplete, e ferramentas de autocomplete com interface própria (inshellisense, Amazon Q/Fig).

O que nenhuma dessas referências tem, e nós já temos, é completion sobre dados do domínio com busca por outro campo: digitar um trecho do título e receber o número.

## 6. O que "melhor do mercado" quer dizer aqui

Proposta de critérios verificáveis, para a meta deixar de ser subjetiva:

1. **Latência:** p95 abaixo de 50 ms de ponta a ponta (shim incluído) com 10 mil tarefas no workspace; nenhuma chamada de rede sem timeout e sem cache.
2. **Cobertura:** toda palavra que o parser aceita tem candidatos ou uma dica do formato esperado, incluindo datas e recorrência.
3. **Fidelidade:** ordem, descrições e escape idênticos ao que o binário decidiu, em todos os shells suportados.
4. **Shells:** bash, zsh, fish e PowerShell com o mesmo comportamento observável; nushell por receita documentada.
5. **Zero manutenção:** comando ou flag novos aparecem sem tocar nos shims (já vale hoje).
6. **Nunca atrapalha:** nenhuma escrita em disco, nenhuma saída em stderr, nenhum travamento (já testado em `TestCompleteWritesNothing`).

## 7. Ordem de trabalho proposta

Cada item é um ciclo de TDD na emenda já existente: `__complete` testado como caixa-preta em `cmd/taskthing/completion_test.go`, mais um teste por shim no shell real.

1. **Diretivas no protocolo.** Uma linha final de diretiva, no estilo do cobra, para o binário pedir "mantenha a ordem", "sem espaço" e nomear grupos. Destrava os itens 2, 4 e 5 sem lógica nos shims.
2. **Ordem preservada** em bash (`nosort`) e zsh (`-V`). Corrige a lacuna 1; é a de maior efeito visível pelo menor custo.
3. **Tarefas sem depender da listagem** (lacuna 2): na falta de número, oferecer por título e completar para o id.
4. **Datas e recorrência** (lacuna 3): candidatos para `d:[`, `r:[`, `set date-time` e `--period`, com a data resolvida na descrição (`tomorrow` → `23 jul`), usando os parsers `chrono` e `rrule` que já existem.
5. **Grupos e descrições melhores** no zsh (`-J`/`-X`), e descrição também com candidato único no bash, quando `COMP_TYPE` indicar listagem.
6. **fish**, que consome o formato atual sem conversão; depois **PowerShell**.
7. **Índice de títulos** para tirar o custo linear: um arquivo derivado em `.taskthing/` gravado pelo `list` (número, id, título, estado), lido em uma única abertura. Só se justifica depois que o item 3 definir o que precisa estar no índice.
8. **Busca tolerante** (lacuna 6): palavras fora de ordem e erro de digitação, com ordenação por relevância, o que depende do item 2.
9. **Cache com validade para `--peer`**, a única chamada de rede.

## 8. Perguntas em aberto

- Sem listagem prévia, completar para o id de 21 caracteres é aceitável, ou o completion deveria numerar por conta própria? Numerar exigiria escrever na kv store, o que hoje é proibido para o completion.
- A meta de latência deve valer para qual tamanho de workspace? O número de 10 mil tarefas é um chute.
- fish e PowerShell entram antes ou depois das melhorias de bash e zsh?
- O formato das diretivas deve ser compatível com o do cobra (para reaproveitar ferramentas como o carapace-bridge) ou próprio? A compatibilidade real está a confirmar.
