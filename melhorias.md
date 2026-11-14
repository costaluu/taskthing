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
