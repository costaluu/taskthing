# Melhorias: ideias para o taskthing

Lista de ideias (2026-11-14), pensada a partir da proposta do projeto: gerenciador de tarefas de terminal, offline-first, sem servidor, sincronizado por git com LWW. Implementado em 2026-11-14 (sem commit ainda): `undo` e as visões `today`, `upcoming`, `overdue` e `week`. O resto não está implementado nem decidido: cada item precisa de spec/ADR antes. O estudo do autocomplete está em `docs/research/autocomplete.md`.

## Ordem sugerida
1. ~~`undo`~~ (feito): um diário local por workspace guarda o valor anterior de cada campo, e o `undo` o regrava como edição nova. O diário foi necessário porque workspaces locais não têm `events.log`.
2. ~~Visões `today`/`overdue`~~ (feito, com `upcoming` e `week`) e saída `--json` (pendente).
3. Lembretes com `notify-send`: o maior buraco de um gerenciador de tarefas sem daemon.
4. Export `.ics`: aproveita o RFC 5545 que já está implementado.
5. TUI interativa: a maior das ideias, por isso por último.

## Uso diário
- `edit <id>` abrindo `$EDITOR` com o `.md` da tarefa (hoje há `set title|description|date-time|board`, um campo por vez).
- Subtarefas e checklists (`- [ ]` na descrição, ou `parent:[id]`); hoje só existe a descrição em texto.
- Prioridade (`p:[high]`) e tags/labels (`#casa`). Boards são exclusivos (uma tarefa em um só), tags não.
- ~~Visões prontas: `today`, `upcoming`, `overdue`, `week`.~~ Feito.
- Busca: `list --search <texto>` ou `find <texto>`.
- ~~`undo`.~~ Feito; não há `redo`.
- Ordenação: `list --sort=date|star|created`.

## Datas e recorrência
- Lembretes/notificações: `taskthing remind` ou `watch` que dispara `notify-send` (ou um hook) para tarefas que vencem.
- Duração, horário, `until` e adiar (`snooze <id> 1d`). Hoje a data é só o `DTSTART`.
- `check` em recorrência: `skip` (pular esta ocorrência) e "completar sem gerar a próxima".
- Exportar para iCalendar (`export --ics`) e feed de leitura para Thunderbird/KDE Calendar, já que o modelo é RFC 5545.

## Sync e multi-máquina
- `sync` automático: `sync --watch` ou uma unit systemd de usuário gerada por `taskthing sync install`.
  - Estado em 2026-11-14: as chaves `autoSync` (padrão 4), `autoRebuild` (5) e `autoTruncate` (10) existem em `config.md`, mas nenhum código as lê. Não há sync automático hoje.
  - Restrição: não pode virar um commit por alteração. O modelo já ajuda, porque cada mudança é uma linha no `events.log` e o commit só nasce no `sync`.
  - Opções a discutir: (a) por contagem, a cada N comandos que alteram algo, como a config prevê, com o custo de o N-ésimo comando esperar a rede; (b) timer de usuário do systemd com debounce, que só sincroniza se houver eventos não publicados e o último sync tiver mais de X minutos; (c) squash dos commits locais ao publicar. Sugestão do agente: (b). Os termos de uso do GitHub sobre frequência de commits não foram verificados.
- `status` (o que está pendente de publicar, quais peers existem) e `log <id>` (histórico de uma tarefa: quem editou o quê).
- `conflicts`: mostrar campos sobrescritos por outro peer pelo LWW, que hoje é resolvido em silêncio.
- `doctor`: valida `events.log`, peers, remote e versão do schema.

## Integração e automação
- `--json` em `list` e nos demais comandos (scripts, barra de status, widgets).
- Importar/exportar: Todoist, Taskwarrior, todo.txt, Markdown puro.
- Hooks (`on-add`, `on-check`), no estilo do Taskwarrior.
- Widget Plasma ou saída para barra de status com as tarefas de hoje.

## TUI
O README fala de TUI e temas, mas os comandos são quase todos de linha de comando. Modo interativo (`taskthing` sem argumentos): navegação por teclado, `space` para marcar, `e` para editar, `/` para buscar. O sistema de temas ANSI já está pronto para isso.
