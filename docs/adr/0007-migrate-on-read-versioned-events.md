# Migração de dados é migrate-on-read com evento versionado

O `events.log` é a fonte da verdade imutável; todo arquivo derivado (`tasks/*.md`, `boards/*.md`, …) é
recomputado por `replay` sob LWW a cada `pull`, `sync` (caso avançado) e `rebuild`. Uma **migração de
dados** — por exemplo `field change-type` num campo cuja mudança de tipo é **não-coercível** — que
reescreve apenas o estado *derivado* com logging OFF (o modelo originalmente descrito na Spec 0005) é
**silenciosamente revertida** no próximo replay: os eventos `CREATE`/`UPDATE_FIELD` no log ainda carregam
os valores no tipo antigo, e o replay reconstrói a partir deles. Isso quebra até para um **único peer
totalmente atualizado**; peers em versões mistas (laggards ainda emitindo eventos no tipo antigo) são um
*agravante*, não a causa. A `z.coerce` na leitura só salva mudanças **coercíveis**. As stories 22
("migrações rodam com logging OFF e nunca vão para o `events.log`") e 32 ("a migração inclui o evento
explícito de reescrita") da Spec 0005 são, portanto, contraditórias: o evento de reescrita não-logado não
persiste para ser replayado.

Decidimos adotar **migrate-on-read com versão por evento**:

- **Cada evento carrega o semver do binário que o gerou.** O binário embeda o mapa `versão → schema`; o
  schema é *dev-authored* e é a fonte única da verdade de tipos, vivendo **apenas** no binário (nunca
  mutável em runtime por workspace).
- **Duas naturezas de migração**, separadas por **se a mudança afeta o replay de eventos de entidade**
  (e *não* por "estrutural vs dados", uma linha que se mostrou imprecisa — ver abaixo):
  - **Transform de replay** (mdwal, Spec 0001) — tudo que remapeia um evento antigo para o schema atual
    da entidade: **`field rename`** (remapeia a *chave* do campo — inclusive os companheiros
    `_lastModified`/`_lastModifiedBy`) e **`field change-type`** não-coercível (reescreve o *valor*). É uma
    **função de transform em código** (TypeScript), escrita pelo dev, chaveada pelo ponto de versão em que
    o schema mudou, aplicada **no replay, por evento**: se `evento.versão < versão atual`, o payload passa
    pela cadeia de transforms (em ordem de versão) antes do apply LWW. Cobre uniformemente o snapshot
    dentro do `CREATE` e o valor do `UPDATE_FIELD`. Vale para `rename` também porque os eventos são
    imutáveis: o log de um peer carrega a chave antiga para sempre; só reescrever os arquivos derivados
    one-shot seria revertido pelo próximo replay — o mesmo bug do `change-type`.
  - **Estrutural one-shot** (runner, Spec 0005) — criar/renomear/apagar **pasta** de entidade fixa,
    regenerar `.gitignore`, editar `config.md`. Não envolve replay de eventos de entidade, então continua
    sendo um **log mdwal estático** autorado por comandos plumber, aplicado **uma vez** com logging OFF e
    gravado em `migrations/`.
  - **Nativo do schema** (sem migração) — **`field create`** com `default` do zod (materializado no read) e
    **`field delete`** (replay ignora evento de campo fora do schema; zod stripa a chave desconhecida).
  Detalhe de correção: originalmente o `field rename` estava classificado como estrutural one-shot; isso
  estava errado pela razão acima e foi movido para transform de replay.
- **Os comandos `field create/rename/delete/change-type` são dev tools de autoria de migração**, não
  capacidade de runtime do usuário final. O usuário nunca edita schema localmente; schema evolui apenas
  via release. `field rename`/`field change-type` fazem o *scaffold*: editam o zod schema, sobem a versão,
  e anexam uma **entrada stub** num registro ordenado de transforms (`src/schema/transforms.ts`), um array
  `{ version, up: (ev) => ev }[]`; cada `up` é **puro e determinístico** (idêntico em todo binário daquela
  versão), o que é condição para a convergência entre peers.
- **Barreira de versão para o problema "para trás".** Migrate-on-read resolve apenas a direção "evento
  antigo lido por binário novo" (o dev tem o transform para subir). O inverso — **evento novo lido por
  binário antigo** — não tem solução por transform (o binário antigo não conhece um schema que ainda não
  existia). Portanto qualquer replay (`pull`/`sync`/`rebuild`) que encontre um evento marcado com versão
  **maior** que a do próprio binário **para com erro claro** ("atualize o taskthing para ≥ `<versão>`") em
  vez de tentar ler às cegas. O `rebuild` recusa se **qualquer** peer tiver evento mais novo que o binário
  do rebuilder (senão geraria um `master` no schema velho, corrompendo os valores novos). O `master`
  carrega a versão em que foi construído; um binário mais antigo que `master` recusa o `pull`. Nunca há
  leitura best-effort de schema futuro. O custo aceito: às vezes o usuário é forçado a atualizar para
  continuar sincronizando.

Isso resolve a contradição story 22/32 na raiz: **não existe mais** "evento de reescrita não-logado que
some" — a reescrita de dados deixou de ser um rewrite one-shot do derivado e virou uma função do caminho
de replay. A `z.coerce` fica apenas como tolerância de leitura **dentro de uma mesma versão** (arquivos
editados à mão / legado), nunca como mecanismo de migração.

Consideramos e rejeitamos as alternativas:

- **Migração logada** (a migração emite eventos reais de reescrita): conflita com "não logar migração" e
  com o LWW — laggards continuariam brigando com os eventos de reescrita, e uma mudança estrutural
  determinística seria confundida com ação de usuário sujeita a LWW.
- **Só barreira de versão** (obrigar todos a atualizar, sem transform): não conserta o replay histórico —
  os eventos antigos no próprio log de um peer atualizado continuam no tipo antigo. A barreira é
  necessária para a direção "para trás", mas insuficiente sozinha.
- **Consolidar-e-esquecer no `rebuild`** (migrar `master`, estabelecer threshold que torne os eventos
  antigos purgáveis): só funcionaria com coordenação global + barreira, e ainda deixaria um laggard
  reintroduzir eventos no tipo antigo. taskthing é explicitamente sem coordenação.

O `clear` **não** é afetado: continua sendo remoção puramente local do arquivo de uma task já
soft-deletada; o log mantém `create` + `deleted=true`, então um `pull`/`rebuild` legitimamente reconstrói
o arquivo. É a mesma observação log-vs-derivado, mas ali o comportamento é intencional, não um bug.

Isto **não** contradiz a [ADR-0001](./0001-lww-plain-timestamp-no-hlc.md) (a comparação de LWW segue
timestamp-vs-timestamp puro; a versão do evento serve só para escolher o transform, não entra no
desempate), a [ADR-0003](./0003-remote-workspaces-sync-events-only.md) (o log continua a única coisa
versionada em `users/<user>`; `master` segue sendo o snapshot derivado, agora carimbado com a versão de
build) nem a [ADR-0006](./0006-per-peer-monotonic-timestamps.md) (a versão do evento é ortogonal ao
timestamp monotônico; qualquer valor reescrito por transform herda o timestamp do evento original, então o
purge/threshold continua válido).
