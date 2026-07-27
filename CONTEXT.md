# taskthing

taskthing é um app CLI que consiste em um task manager baseado em git sem necessidade de coordenação central. a cli será toda em inglês.

## Language

**LWW (Last Write Wins)**:
Política de resolução de conflito entre edições concorrentes de um mesmo campo: vence o evento com o timestamp (nanosegundos) mais recente. Não usa HLC nem CRDT — ver [ADR-0001](./docs/adr/0001-lww-plain-timestamp-no-hlc.md).

O controle de LWW é feito por campo, denormalizado no próprio frontmatter: para cada campo do schema (ex: `title`), o mdwal mantém metadados companheiros (ex: `title_lastModified` em nanosegundos, e o autor da última mudança). Esses metadados existem em todo workspace, local ou remoto — a diferença entre os dois não está na forma do arquivo, só em escrever ou não no `events.log`. O autor vem da configuração global do git (`user.name`) da máquina, mesmo em workspaces locais sem repositório git — ver [ADR-0002](./docs/adr/0002-author-from-global-git-config.md).

**Timestamp monotônico (por peer)**:
Todo evento nasce com um timestamp em nanosegundos UTC gerado como `max(now_utc_ns, último_ts_local_ns + 1)`, onde `último_ts_local_ns` é a última linha do próprio `events.log` deste peer. Garante que o log de cada peer seja **estritamente crescente**, mesmo que o relógio de parede UTC ande pra trás (NTP em step, restore de VM, acerto manual). É essa invariante que torna o **purge por timestamp** seguro: como o log é monotônico, `note[peer]` (o máximo consolidado por um `rebuild`) equivale à fronteira do prefixo consolidado, e todo evento anexado depois tem `ts > note[peer]` por construção — nunca é purgado por engano. Também elimina auto-inconsistência de LWW entre edições sucessivas do mesmo peer no mesmo campo. Não é um HLC: não há contador lógico nem causalidade trocada entre peers — cross-peer o LWW continua timestamp-vs-timestamp puro (ver [ADR-0001](./docs/adr/0001-lww-plain-timestamp-no-hlc.md) e [ADR-0006](./docs/adr/0006-per-peer-monotonic-timestamps.md)).

**Evento (mdwal)**:
Registro imutável de uma ação **autorada por este peer**, não de uma mudança de estado efetiva. O `events.log` de uma branch `users/<user>` contém **exclusivamente os eventos autorados por aquele usuário** (mais os `criar` retroativos do init 1.2, que também são dele) — eventos de outros peers **nunca** são copiados para cá; eles chegam já consolidados via snapshot da branch `master` (ver `manager sync`/`rebuild`). Um evento é gravado no log verbatim e permanece lá mesmo que **perca** a disputa de LWW no replay (ex: outro campo com timestamp mais recente venceu) — apenas não é aplicado ao estado. Assim, o log é a fonte de verdade de tudo que **este peer produziu**; o estado atual é derivado do snapshot da `master` mais o replay determinístico do LWW sobre os eventos deste peer ainda não consolidados.
_Avoid_: "mudança", "mutação aplicada" (implica que o evento sempre alterou o estado, o que nem sempre é verdade); descrever o log como "tudo que o peer observou/recebeu" — ele só guarda o que o próprio peer autorou, nunca o que recebeu de outros

Um evento é sempre atômico e de um dos tipos: **criar** (carrega o snapshot inicial completo da entidade, nunca decomposto em N eventos de campo), **atualizar campo** (um único campo por evento — remoção é soft-delete, ou seja, é só uma atualização de campo, nunca remoção física do arquivo), ou uma operação de pasta (renomear/truncar). Nunca existe um evento que combine múltiplas dessas ações.

Formato de uma linha do log: `<timestamp_ns>::<op>::<entity_type ou nome-da-pasta>::<entity_id ou vazio>::<payload_json>`. O cabeçalho (4 campos separados por `::`) é sempre de tamanho fixo; qualquer conteúdo arbitrário do usuário (valor de campo, autor, snapshot) vai dentro do `payload_json`, nunca no cabeçalho — evita colisão com `::` dentro de texto livre.

A própria pasta do workspace é tratada como só mais uma pasta sujeita a `RENAME_FOLDER`/`TRUNCATE_FOLDER` — mesmo essa operação gera uma linha no `events.log` daquele workspace (o log continua vivendo dentro da pasta durante e depois do rename). Nessas linhas, a posição de "nome-da-pasta" é o sentinela fixo `$root` (nunca o nome atual/anterior da pasta) — evita ambiguidade em replays com múltiplos renames em sequência.

Aplicar um evento de **atualizar campo** é uma única escrita atômica no arquivo, que atualiza o valor do campo e seus metadados de LWW (`<campo>_lastModified` a partir do timestamp do próprio cabeçalho da linha, `<campo>_lastModifiedBy` a partir do `author` do payload) todos juntos — nunca 3 eventos separados para 1 mudança de campo.

Eventos só existem em **workspaces remotos**. Num workspace local, toda operação (incluindo truncate) é aplicada direto no arquivo, sem passar pelo `events.log` — não há LWW a resolver porque não há coordenação entre peers.

**Workspace**:
Uma pasta gerenciada pelo taskthing (nome livre, pode ser criada/renomeada/deletada pelo usuário) contendo `tasks/` e `boards/` (pastas fixas por schema, truncáveis mas não renomeáveis/deletáveis diretamente). Pode ser **local** (sem remoto associado — mdwal escreve direto nos arquivos, sem `events.log`) ou **remoto** (associado a um repositório git remoto — mdwal registra todo evento em `events.log` para permitir LWW entre peers).

Num workspace remoto, o git nunca versiona o estado derivado: toda pasta fixa de entidade (`tasks/`, `boards/`, e quaisquer outras introduzidas por migração) fica no `.gitignore`, cujo conteúdo é derivado do schema atual (regenerado automaticamente a cada migração que altera o conjunto de pastas fixas) — só `events.log` é rastreado. O conteúdo dessas pastas é sempre reconstruído via replay do log, nunca commitado diretamente. A branch `master` do `manager` é a exceção: nela os arquivos derivados são commitados de fato, como snapshot de bootstrap para novos peers — ver [ADR-0003](./docs/adr/0003-remote-workspaces-sync-events-only.md).
_Avoid_: "pasta", "projeto"

**Id de entidade**:
Um nanoid de 21 caracteres, gerado uma única vez na criação de uma task/board e usado como nome do arquivo (`<id>.md`). É estável durante toda a vida da entidade — nunca muda com edições de conteúdo, ao contrário de um hash de conteúdo.
_Avoid_: "hash" sozinho (o `CONTEXT.md` original usa "hash", mas é um id aleatório, não um digest derivado de conteúdo)

**Data da task**:
Uma task não tem campo de data próprio — a única coluna temporal do schema é `rrule`. A **data** de uma task é o componente `DTSTART` do `rrule`; a **recorrência** é o componente `RRULE` (pode não existir). Uma task datada porém não-recorrente é um `rrule` só com `DTSTART` (sem regra recorrente); uma task sem data é `rrule: null`. O `DTSTART` de uma task aberta é **fixo** (pinned): nunca avança sozinho com a passagem do tempo — só avança quando a task é completada e gera a próxima ocorrência (ver recorrência na seção `models`). Assim, uma ocorrência recorrente não-checada permanece com sua data original e renderiza corretamente como atrasada ("N days ago"), em vez de silenciosamente pular para o futuro. A data mostrada de uma task completa é sempre o timestamp de completação (o `lastModified` do campo `completed`), nunca a rrule. `set date-time` reescreve o `DTSTART`.
_Avoid_: adicionar um campo `date`/`dueDate` paralelo ao `rrule` (criaria duas fontes de verdade para "quando")

**mdwal**:
Motor genérico de baixo nível que executa qualquer operação sobre a estrutura de um workspace: criar/renomear/truncar pastas, criar/atualizar/remover (soft) campos de frontmatter em arquivos markdown, e resolver LWW. É acoplado a um schema (zod) que descreve os campos válidos de cada tipo de entidade (task, board, ...) e gera os metadados de LWW correspondentes. Comandos **plumber** do taskthing expõem essas operações diretamente ao usuário.

**Migração**:
Um log mdwal estático, autorado com comandos plumber e embutido no binário, vinculado a uma versão específica do taskthing. É replayado uma única vez contra um workspace durante o auto-update (ex: para criar uma pasta `lists/` nova em todos os workspaces). Não é logada no `events.log` do workspace — é uma transformação estrutural determinística que todo peer aplica igualmente ao atualizar o binário, não uma ação de usuário sujeita a LWW. Migrações também podem alterar `config.md` (adicionar/remover/renomear campos de configuração, ou restringir valores antes válidos — ex: remover a opção de formato de data americano, deixando só o europeu). Quando um valor existente deixa de ser válido no schema novo, não há coerção automática: a migração precisa incluir explicitamente o evento que reescreve o valor antigo para um válido (o mdwal nunca adivinha como migrar um valor sozinho — quem decide é sempre a migração).

O binário é sempre a fonte de verdade de qual migração existe e o que ela faz. Cada workspace mantém uma pasta `migrations/` local só para **marcar quais migrações já foram aplicadas ali** (um arquivo `<migration_id>.md` por migração aplicada, guardando uma cópia congelada do que rodou) — não é de onde a migração é lida para aplicação, só um registro de aplicação. Cada peer decide independentemente, a partir do seu próprio binário, quais migrações já rodou; não há necessidade de sincronizar isso entre peers, então essa pasta segue a mesma regra de ADR-0003 (fica no `.gitignore`, nunca é commitada em `users/<user>`).

**Tema**:
Remapeamento dos papéis semânticos da UI (ex: "destaque", "erro", "texto secundário") para os 16 índices de cor padrão do ANSI — nunca valores RGB/true-color arbitrários. A paleta real de cada índice já é definida pelo emulador de terminal do usuário (ex: Dracula, Solarized); o taskthing só decide qual papel usa qual índice, preservando consistência com o resto do terminal. Configurado em `config.md`, 100% local por usuário — ver [ADR-0005](./docs/adr/0005-ansi-only-no-named-presets.md).
_Avoid_: "tema RGB", "true color" (fora de escopo)

**config.md**:
Arquivo de configuração global do taskthing, fora de qualquer workspace (`~/.config/taskthing/config.md`). Markdown+frontmatter validado por schema zod como qualquer outro arquivo do mdwal, mas **fora do domínio de LWW/`events.log`** — não é compartilhado via git remoto, então não há edição concorrente a resolver; é escrita direta, como em um workspace local. Só muda por ação direta do usuário na máquina ou por migração.

# requisitos iniciais

- os eventos descentralizados produzidos pelo mdwal devem possuir precisão de nanosegundos para viabilizar política de LWW (Last Write Wins)
- taskthing é uma aplicação escrita em typescript usando bun.
- taskthing deve ser uma aplicação de dois níveis assim como git: plumber (atua nas funcionalidades core) e porcelain (atua por cima das funcionalidades plumber para facilitar para o usuário)
- taskthing deve ter um tema altamente customizável (tema ANSI de 16 cores — ver [Tema](#language))
- taskthing deve ser um binário único servido para linux, mac e windows
- taskthing deve ser capaz de gerenciar multiplas pastas (workspaces) sendo ela local ou não (crud)
- taskthing deve ser capaz de associar um repositório remoto e desassociar um repositório remoto a qualquer tempo (em caso de desassociação, o conteudo remoto deve ser preservado não precisa excluir o conteudo remoto)
- taskthing deve ser capaz de realizar o setup de um repositório git a partir de um URL em remote
- todos os arquivos gerados manipulados pelo taskthing serão markdown com yaml frontmatter.

# níveis de implementação

## binários e distribuição

- esse reposítorio será hospedado no github, por isso, você deverá fazer um arquivo de workflow para que seja possível baixar binários para windows, linux e mac
- cada binário deve ter um sistema de vesionamento semver.
- taskthing deve ser capaz de se auto atualizar através do github. ou seja, ser capaz navegar até a ultima release verificar se o binário para do SO está em uma versão superior. se sim, realizar o download, atualização do binário local e migrações se existirem.

## migrações

as migrações serão embedadas no próprio binário. os arquivos de migrações serão vincadas as versões idealmente 1 arquivo de migração será vinculada a uma vesão especifica. Esses arquivos gerados pelo mdwal que será um motor de edição de markdown com yaml frontmatter que ao realizar as edição gera um log WAL que será a fonte das migrações.

## mdwal (core)

mdwal é um motor de edição de markdown com frontmatter e além disso esse motor será responsável pela criação, renomeação e remoção de arquivos. mdwal é o motor principal que viabiliza o aplicativo taskthing através da produção de logs WAL. Basicamente cada execução desse motor ele produz uma nova linha em um arquivo, se necessário.

Esse log é permite que vários usuários, sem coordenação, consigam compartilhar um mesmo folder (workspace).

### funcionamento do mdwal

o mdwal ele deve garantir algunas coisas:

- cada evento é readonly assim como WAL file
- para cada operação realizada ele pode ou não escrever no log (por exemplo operações migrações executadas não devem ser logadas no arquivo, assim como demais operações internas)
- o mdwal deve ser capaz de realizar o parse de um log WAL
- o mdwal deve possuir 2 níveis de operações:
  - operações que criam/renomeiam/deletem/truncate pastas
  - operações que manipulam dados diretamente sendo criando arquivos markdown e seu frontmatter ou atualizando esses arquivos.
- cada evento no mdwal terá precisão de nano segundos para viabilizar política de Last Write Wins.
- sobre a politica de LWW: cada evento no mdwal deve ser atómico, isto é, será a atualização de um campo criação remoção etc em um único evento ele nunca terá N ações. Para executar essa ação, ele deve primeiro verificar se essa ação é mais recente do que a registrada (atual) e se sim deve executar a ação, se não ele simplesmente deve ignorar a ação e gravar no log como executada.
- o mdwal deve operar sobre um schema variavel de arquivos usando zod. sendo capaz de ler (transformando em json) / escrever (transformando em markdown com frontmatter).
- o formato do log do mdwal deve seguir espaçadores "::" deve registrar o timestamp do evento em nanosegundos seguido das operações operandos e etc

## manager (core)

o manager será a parte do projeto para lidar com as operações com o git. o repositório remoto consistirá na seguinte estrutura:

```
master/
users/alice
users/bob
users/charlie
```

restrição importante: na branch master, não poderá haver logs registrados apenas o arquivo vazio afinal de contas a branch master servirá exclusivamente para usuário construirem suas branches users por ser um estado consistente.

onde na branch master, existirá um estado consistente dos logs de todos os usuários criados pelo comando rebuild. com essa arquitetura usuário podem facilmente se conectar a um repositório existente e trabalhar independentemente em suas branches garantindo assim consistencia eventual desejada.

basicamente ele consistirá em 3 principais operações:

1. init

como parâmetro recebido, o usuário deverá informar o remote URL e obviamente ter sua configuração git configurada corretamente

essa operação será responsável por iniciar então o repositório criando a estrutura inicial separando em casos distintos:

1.1 repositório remoto vazio

nesse caso o taskthing deve criar a estrutura inicial, criando a branch master utilizando com estado consistente a pasta do workspace atual do usuário sem os eventos. e criando a branch users/<user> onde o usuário passará a trabalhar.

1.2 repositório remoto existente sem a branch do usuário presente

o usuário pode ter trabalho prégresso: um workspace local (nunca antes associado a nenhum remoto) já com tasks/boards criados. Nesse caso, o conteúdo local é preservado: no momento da associação, o mdwal emite retroativamente um evento `criar` (com snapshot completo) para cada entidade local já existente, timestampado no momento da associação, e grava esses eventos no `events.log` recém-criado da branch `users/<user>`. Isso garante que o trabalho prévio do usuário se torne parte do histórico compartilhado e outros peers eventualmente sincronizem essas entidades — sem isso, o conteúdo local ficaria invisível para sempre aos demais peers, contradizendo o próprio propósito de associar a um remoto existente.

1.3 repositório remoto existente com a branch do usuário presente

ainda não pensei muito bem em todos os detalhes, mas o caso base é: o usuário já tem uma branch `users/<user>` no remoto (histórico real e potencialmente conflitante) e também tem trabalho local pregresso. Aqui não há como reconciliar as duas histórias, então o remoto prevalece: o taskthing exige confirmação interativa explícita do usuário (sem flag de bypass) antes de prosseguir, e ao confirmar, apaga o conteúdo local (`tasks/`, `boards/`) permanentemente — sem backup — em favor do checkout da branch remota existente.

2. sync

existem dois cenários aqui:

2.1 o sync foi feito sem que haja uma nova versão do snapshot consistente na branch master

esse é o caso mais simples e simplesmente taskthing deve fazer add, commit e push para a branch com uma mensagem de commit

2.2 o sync foi feito com uma nova versão do snapshot consistente na branch master

nesse caso, taskthing deve basicamente deve:

- fazer o purge do eventos mais velhos que a informação salva na branch master que terá o formato { user1: timestampinns, user2: timestampinms... } ele deve pegar essa timestamp e fazer o purge (essa é mais uma operação interna que o mdwal deve ser capaz de fazer)
- baixar o estado da branch master (ver `manager pull` abaixo)
- reaplicar os eventos mais recentes que o registrado na master (sem necessidade de logar novamente no log)

o comando sync deverá ser executado em nível de porcelain para manter o repositório atualizado em tempos em tempos

**`manager pull`** (plumber): primitivo que faz só "baixar/checkout do estado da `master` + replay determinístico local", sem purge e sem publicar nada (equivalente a `git fetch`/`git pull` sem push). `sync` (porcelain) compõe esse primitivo internamente no cenário 2.2, junto com o purge. Por ser plumber, `pull` também fica disponível como comando direto pro usuário avançado que queira forçar um replay do estado da master sem disparar um sync completo.

3. rebuild

o comando de rebuild serve para construir o estado consistente entre os diversos usuários e reescrever na branch master. qualquer usuário pode fazer o rebuild e basicamente a qualquer momento. inclusive no config.md o usuário terá a opção de realizar o rebuild automaticametne após x quantidades de sync.

todos os logs de todos os usuários são coletados e combinados inteiramente em memória (RAM) — nada é escrito em disco nas branches `users/<user>` de terceiros durante esse processo, só se faz checkout/parse para leitura.

o comando consiste em:

1. realizar um sync
2. salvar em memória todos os logs de todos os usuários
   2.1 para isso, ele deverá: fazer checkout em cada branch fazer o parse dos logs e concatenar com os seus logs, ao final o usuário responsável pelo rebuild ordena os logs
   2.2 ele deve guardar os ultimos logs de cada usuário para colocar uma note no commit da branch master aquele objeto { user1: timestampinns, user2: timestampinms... } para que outros usuários saibam com precisão até que timestamp os seus eventos podem ser purgeados.
3. com os logs coletados e ordenados (em memória), o mdwal aplica essas mudanças à `master`, gerando o estado consolidado (arquivos derivados, exceção de ADR-0003).
4. com um estado consolidado ele commita na master o estado com os eventos limpos. o push para `master` é sempre não-fast-forward: se rejeitado (outro usuário rebuildou em paralelo), o rebuild inteiro é refeito a partir do passo 2 contra a nova `master`.
5. volta para sua própria branch de trabalho e realiza um `sync` normal — é esse sync (não o rebuild em si) que efetivamente purga a própria branch do rebuilder, usando o novo threshold que ele acabou de publicar. `rebuild` não reescreve nenhuma branch `users/<user>` diretamente, nem mesmo a do próprio rebuilder.

6. truncate history

a operação de truncate history serve justamente para evitar que o repositório cresça indetermiandamente e mantém os últimos N commits de uma branch. um usuário só pode truncar a própria branch `users/<user>` ou a `master` — nunca a branch de terceiros. Não é oferecida a opção de truncar além do último ponto consolidado por um `rebuild` conhecido (ou seja, nunca é possível destruir eventos que ainda não foram vistos por nenhum outro peer via `master`).

## configuração e temas

as configurações de cada usuário são 100% locais, isso significa que cada usuário pode estar usando configurações diferentes e mesmo assim compartilharem o mesmo workspace. nas configurações deve-se haver configurações de:

1. binário

que lida com versões do binário, quando foi a ultima vez que uma atualização foi verificada, e se o auto-update é silencioso ou requer confirmação (padrão: avisa e pede confirmação antes de baixar/aplicar — uma atualização pode disparar migração estrutural irreversível, então não deve ser silenciosa por padrão). o usuário pode optar por auto-update silencioso via `config.md`.

2. configurações padrão

basicamente seria a configuração do formato de data e hora, o workspace atual que está sendo utilizado e o tema de 16 bit de cores que o usuário está utilizando na CLI.

essas configurações precisam ser 100% customizáveis seja com temas pré-definidos ou 100% customizado. datetime formato terá duas opção que é o formato americano ou europeu e o workspace atual o proprio usuário decidirá qual workspace ele utilizará.

## estrutura

no campo estrutura essa seria a referência de uma pasta taskthing

~/.config/taskthing

- config.md
- local/
  - tasks/
    <task1_hash>.md
    ...
    <taskN_hash>.md
  - boards/
    <board1_hash>.md
    ...
    <boardN_hash>.md
  - migrations/
    <migration_id>.md
    ...
    <migration_id>.md
- my-remote-workspace/
  - events.log
  - tasks/
    <task1_hash>.md
    ...
    <taskN_hash>.md
  - boards/
    <board1_hash>.md
    ...
    <boardN_hash>.md
  - migrations/
    <migration_id>.md
    ...
    <migration_id>.md

## models

`createdAt`/`updatedAt` não são campos do schema de domínio — são derivados automaticamente pelo mdwal (`createdAt` = timestamp do evento `criar`; `updatedAt` = maior `<campo>_lastModified` entre todos os campos da entidade), o mesmo mecanismo que já gera `<campo>_lastModified`/`<campo>_lastModifiedBy` por campo.

tasks:

const \_taskSchema = z.object({
id: z.string().nonempty(),
board: z.string().nonempty().default("inbox"),
completed: z.boolean(),
title: z.string(),
star: z.boolean(),
rrule: rruleSchema.nullable(),
description: z.string().nullable(),
deleted: z.boolean(),
})

Para cada task deverá existir o board chamado inbox, basicamente o board inbox trata-se de um board virtual, ou seja, o workspace não precisa ter o board inbox criado. sendo assim o campo board comporta o id do board. e como os ids são nanoids, não existirá um board com id "inbox" apenas o virtual que estamos definindo agora.

O campo `board` é, portanto, uma string sentinela: normalmente contém o nanoid de um board real, mas o valor literal `"inbox"` é reservado para o board virtual e nunca aparece como id de um board de verdade. Essa invariante deve ser respeitada pelo gerador de nanoid (na prática, a chance de colisão é desprezível, mas o valor `"inbox"` nunca deve ser tratado como um nanoid válido gerado para um board real) — ver [ADR-0004](./docs/adr/0004-board-sentinel-inbox.md).

boards:

const \_boardSchema = z.object({
id: z.string().nonempty(),
name: z.string(),
icon: z.string(),
color: z.string(),
deleted: z.boolean(),
})

const migrationSchema = z.object({
version: z.string(), // semver
content: z.string().nonempty(), // logs
})

interface de cada repositório

interface Repository<Model> {
create(task: Model): Promise<Model | null>
update(task: Model): Promise<boolean>
delete(id: string): Promise<boolean>
recover(id: string): Promise<boolean>
findById(id: string): Promise<Model | null>
findAll(): Promise<Model[]>
filter(predicate: (value: Model) => boolean): Promise<Model[]>
}

const rruleSchema = z.string().transform((str) => {
try {
return RRule.fromString(str)
} catch {
return z.NEVER
}
})

completar uma task com `rrule` não reseta/reaproveita a entidade existente: cria-se uma **nova** entidade (novo id, novo evento `criar`, herdando o mesmo `rrule`) para a próxima ocorrência, enquanto a task original permanece marcada como `completed`. Preserva o histórico de cada ocorrência individualmente, e é consistente com o resto do domínio (id de entidade nunca é reciclado/reaproveitado).

O `DTSTART` da nova entidade é a **próxima ocorrência da `RRULE` após o `DTSTART` da entidade completada** — avança exatamente uma ocorrência, mesmo que essa próxima ocorrência também já esteja no passado. Isso permite "colocar em dia" uma task recorrente atrasada checando-a repetidamente (cada check consome uma ocorrência atrasada de cada vez), em vez de pular direto para a próxima ocorrência futura. Se a `RRULE` estiver esgotada (sem próxima ocorrência), completar não gera nova entidade.

## kv store

no campo das kv stores vamos falar de detalhes mais específicos.

basicamente, essa kv store serve para evitar que o usuário tenha que usar os identificadores das tasks que são nanoid então basicamente quando o usuário listar as tasks ou boards essa kv store irá entrar em cena para mapear os nanoids para numéros palatáveis: 1, 2, 3... ou seja o kv_store mapeia o (1 -> nanoid) (2 -> nanoid)...

os números são temporários (efêmeros): resetados a cada vez que o usuário roda um `list` no workspace — não são um id estável de longo prazo, só uma referência ergonômica válida até a próxima listagem, sem proteção contra colisão entre terminais/processos concorrentes na mesma máquina — o `list` mais recente vence, mesmo trade-off já aceito para a numeração efêmera em geral. Existem 3 kv stores independentes — uma para tasks, uma para boards e uma para migrations (essa última alimenta um futuro comando de status/histórico de migrações aplicadas no workspace, numeradas pra referência em vez do `migration_id`/semver completo) — cada uma persistida em seu próprio arquivo `.json` dentro do workspace. Esses 3 arquivos ficam no `.gitignore` (mesma lógica de ADR-0003: são estado derivado/local, não fonte de verdade). O momento exato em que os números são (re)gerados é decisão de comando **porcelain**, fora de escopo aqui.

a store deve implementar os métodos save, load, set, get e reset. `get` é bidirecional (número → nanoid, e nanoid → número) — necessário porque `list` precisa do sentido nanoid → número pra popular a store enquanto itera as entidades, e a resolução de comandos precisa do sentido número → nanoid.

taskthing é um CLI sem processo de longa duração — não há estado em memória entre comandos, cada invocação é um processo novo. Por isso a store não é cacheada em RAM: toda vez que a feature é usada (ex: resolver `task 3`), uma nova leitura do `.json` no disco é feita para pegar o mapeamento atualizado. `reset` também opera direto no disco (não só em memória) — se `list` crashar no meio da repopulação, o `.json` pode ficar vazio/inconsistente até a próxima listagem bem-sucedida. Isso é aceitável pelo mesmo raciocínio já adotado para a store como um todo: é uma referência de curtíssimo prazo, não fonte de verdade.

## comandos

dois níveis, sem namespace explícito prefixando os comandos (mesmo padrão do git: plumbing e porcelain convivem no mesmo binário/CLI, sem prefixo tipo `taskthing plumber ...`) — a aplicação já é nichada o suficiente pra não precisar reforçar a distinção na sintaxe.

decisão deliberada: as duas camadas usam convenções de sintaxe diferentes para referenciar a entidade — plumber recebe o nome da entidade como argumento posicional (`taskthing field create <entity-name> ...`), porcelain como flag (`taskthing <operation> --<entity_name> ...`). não é inconsistência a corrigir; é a forma escolhida de reforçar visualmente, no próprio comando, em qual camada o usuário está operando.

### plumber (baixo nível, opera direto por nanoid/id completo, sem `kv_store`, sem confirmações interativas — só validações):

#### entity

essa classe de comandos é especialmente utilizado para criar migrações ou aplicar mudanças em baixo nível

1. taskthing entity <create|rename|delete> <name> --log

esse comando será responsável por realizar o CRUD das entidades novas, reforçando entidades são como tasks e boards. nas condições padrões, esse comando será utilizado quando houver desenvolvimento que suporte a utilização dessa entidade.

- o create será responsável por criar as pastas com o nome da entidade em todos os workspaces
- o rename será responsável por renomear as pastas com o nome da entidade em todos os workspaces
- o delete será responsável por deletar as pastas com o nome da entidade em todos os workspaces

como comando plumber, ele basicamente servirá para criação de migrações tendo nenhuma utilidade para o usuário

2. taskthing field create <entity-name> <field-type> --log

o schema Zod é a única fonte de verdade do tipo do campo — o YAML do frontmatter nunca carrega informação de tipo junto ao valor, só o valor bruto. `field create` registra a definição do campo (nome + tipo Zod) nos metadados de schema da entidade (parte do código/migração, não um dado do workspace); ao ler um arquivo, o mdwal usa o schema Zod vigente para fazer parse/coerção do valor YAML (ex: `z.coerce.number()` se o YAML trouxer string mas o tipo declarado for number).

3. taskthing field delete <entity-name> --log

comando baiscamente auto-explicativo.

4. taskthing field rename <entity-name> <new-name> --log

comando baiscamente auto-explicativo.

5. taskthing field change-type <entity-name> <new-field-type> --log

por ser mudança do tipo declarado no schema Zod (a fonte de verdade), `field change-type` é fundamentalmente uma migração: precisa reescrever o valor de cada entidade existente para o novo tipo — mesmo raciocínio já aplicado a mudanças de schema em `config.md` (a migração inclui explicitamente o evento que reescreve o valor antigo para um válido; o mdwal nunca infere a coerção sozinho).

#### manager

1. taskthing manager init|sync|rebuild|pull|truncate --branch

comandos basicamente auto-explicativo.

a opção branch é completamente facultativa, pois implicitamente os comandos do manager irão olhar no config.md

#### config

taskthing config get|set <key> [value]
taskthing config delete <key>

comandos praticamente auto explicativos

### porcelain (verbos diários, usam `kv_store`, números em vez de nanoid):

#### setup

1. taskthing install

esse comando será responsável basicamente por realizar o scafold inicial do projeto indo na pasta home do usuário e criando a pasta taskthing com o workspace local. também haverá um breve formulário intuitivo para que o usuário defina o formato de data e hora sendo as opções america ou europa (UI de seleção, mono-select). outra opção que será mantida é se o usuário suporta nerdfonts pois esse projeto fará uso intensivo dela — pergunta via UI de confirmação (y/n), default para não (assume sem suporte a nerdfont até o usuário confirmar que tem).

#### update

1. taskthing update check

comandos auto explicativos, na config deverá ser salvo se há uma atualização disponível ou não assim como a hora em que foi feita a ultima checagem. se tem menos que 12h que foi checado não precisa verificar basta dar a resposta salva na configuração do usuário

2. taskthing update apply

usa o update check por debaixo dos panos, se for identificado que existe uma atualização ele baixa e atualiza o binário.

#### workspace

1. taskthing workspace list|use|create|rename|delete

comandos basicamente auto explicativos. exceções: `delete` passa pela UI de confirmação (destrutivo, apaga a pasta local) antes de executar; e deletar o workspace atualmente ativo (setado via `use`) é bloqueado com erro — o usuário precisa trocar de workspace ativo antes de poder deletá-lo, pra nunca ficar num estado sem workspace corrente.

2. taskthing workspace remote add|remove <remote_url>

comandos basicamente auto explicativos.

3. taskthing use <workspace-name>

atalho de primeira classe para `workspace use`, no mesmo padrão de `taskthing boards` e `taskthing theme`. a troca do workspace atual é feita **exclusivamente** por este comando (ou seu equivalente `workspace use`) — não fica acessível via TUI genérica de `config`, mesmo sendo tecnicamente um valor persistido em `config.md`.

#### entities

embora usuários em teoria podem criar suas próprias entidades somente o desenvolvedor poderá adicionar suporte as entidades via código. para começar o taskthing dará suporte a 3 entidades distintas que são: tasks, boards e migrations.

a estrutura do comando será feita da seguinte forma:

taskthing <operation> --<entity_name> <parameters>

##### tasks

para as tarefas devido ao suporte de recorrência com rrule, será necessário parsear datas e rrule de forma precisa para isso quero que seja utilizada as seguintes bibliotecas:

https://github.com/jkbrzt/rrule
https://github.com/wanasit/chrono

segue os comandos:

Observação muito importante pelo fato do taskthing ser um taskmanager a flag <entity_name> por padrão será "task". por outro lado a flag <workspace-name> será opcional visto que na configuração existe o workspace atual.

1. taskthing add <string-input> --workspace=<workspace-name>

Nessa string input serão extraidos a possível existência de rrule e data natural respectivamente tudo em inglês.
Nessa string input, o usuário deve fornecer a rrule e data com formatos específicos. vamos aos exemplos:

"walk the dog" -> cria a task só com o titulo
"walk the dog d:[tomorrow]" ou "walk the dog date:[tomorrow]" -> cria a task com titulo e rrule com data mas com recorrência única.
"walk the dog r:[every monday]" ou "walk the dog recurrence:[every monday]" -> cria a task com título e rrule definida.
"walk the dog d:[tomorrow] recurrence:[every monday]" -> nesse caso a dtstart é justamente a data fornecida.

os colchetes são obrigatórios e fazem parte do reconhecimento do padrão: o parser só extrai `d:`/`date:`/`r:`/`recurrence:` quando seguidos de `[...]` fechando. um texto livre como `"remind Ed: buy milk"` não bate no padrão (não tem colchetes) e permanece como parte do título — é também o que torna trivial remover esses trechos de flag do texto final do título, já que o par tag+colchetes é sempre removido inteiro.

2. taskthing star <id> --workspace=<workspace-name>

3. taskthing delete <id> --workspace=<workspace-name>

4. taskthing check <id> --workspace=<workspace-name>

5. taskthing set date-time <id> --workspace=<workspace-name>

6. taskthing set description <id> --workspace=<workspace-name>

7. taskthing set title <id> --workspace=<workspace-name>

8. taskthing clear <id> --workspace=<workspace-name>

`clear` é uma limpeza **puramente local**: remove de vez uma task que **já está soft-deletada** (`deleted: true`) apagando o arquivo derivado local dela. Não emite evento, não passa por LWW e não propaga para outros peers — só tira o lixo de tasks deletadas da máquina local. Só se aplica a tasks já deletadas (chamar `clear` numa task não-deletada é erro). Num workspace remoto, como o estado é derivado do replay do log, os eventos `criar`/`deleted` daquela task continuam existindo — então um `pull`/`sync`/`rebuild` posterior pode reconstruir o arquivo localmente de novo; `clear` é tidy-up de curtíssimo prazo, não uma remoção definitiva do histórico.

9. taskthing uncheck <id> --workspace=<workspace-name>

10. taskthing list --checked --starred --period=<1d,2d...Nd|1m...Nm|1y...Ny> --deleted --hasDescription --in-board=<board-name>,<board-name>... --workspace=<workspace-name>

por padrão, --checked está como falso

`--in-board` é um nome deliberadamente diferente de `--board` (o seletor de entidade usado em `list --board` pra listar boards) — evita colisão de flag com dois significados (seletor de entidade vs. filtro por valor) sob o mesmo comando `list`.

`--in-board=inbox` funciona como filtro especial: compara o campo `board` da task direto com a string sentinela `"inbox"`, sem passar pelo `kv_store`/nanoid de um board real (já que "inbox" não é uma entidade board de verdade — ver definição do campo `board` na seção `Language`/schema de task).

##### boards

1. taskthing add --board <board-name> --workspace=<workspace-name>
2. taskthing set --board name <id> <new-name> --workspace=<workspace-name>
3. taskthing set --board icon <id> <icon-string> --workspace=<workspace-name>
4. taskthing set --board color <id> <ansi-color-name> --workspace=<workspace-name>

a cor do board também é ANSI 16 (um dos índices 0-15, escolhido via UI de seleção entre um conjunto pré-definido de nomes de cor), consistente com a ADR-0005 — nunca hex/RGB arbitrário. dá ao usuário alguma opção de escolha sem reabrir a exceção de RGB.
5. taskthing delete --board <id> --workspace=<workspace-name>

6. taskthing list --board --workspace=<workspace-name>

um atalho para esse comando será o "taskthing boards"

##### migrations

1. taskthing migrations

basicamente mostra as migrações aplicadas ao workspace atual e verifica se a todas as migrações possíveis foram aplicadas.

#### manager

1. taskthing sync
2. taskthing rebuild
3. taskthing truncate [--branch <b>]

#### config e temas

arquitetura de comandos decidida:

- `taskthing config` sem argumentos → abre a TUI interativa completa, navegando por todas as chaves configuráveis exceto o workspace atual (formato de data, nerdfont on/off, tema) — o workspace atual é excluído dessa TUI e só é trocado via `taskthing use`/`workspace use` (ver seção `workspace`).
- `taskthing config get/set/delete <key> [value]` (porcelain) mantém a mesma sintaxe do comando plumber equivalente, mas com validação/confirmação amigável, sem abrir TUI — uso scriptável.
- `taskthing theme` é atalho de primeira classe pra `config` filtrado só na parte de tema (pula direto pro mapeamento padrão vs. custom+textarea), mesmo padrão do atalho `taskthing boards`.

sem presets nomeados (catppuccin, gruvbox etc — decisão revista, ver [ADR-0005](./docs/adr/0005-ansi-only-no-named-presets.md)): só existe **um mapeamento padrão** de papel→índice ANSI (que já deve funcionar bem em qualquer esquema de terminal, já que os índices seguem convenção semântica comum entre temas populares) e a opção **customizada**, pra quem quiser um tweak específico.

para a opção customizada deve-se colocar uma text-area e receber um json com as cores ansi 16 bits. o json é compatível com a definição de **Tema** no glossário: mapeia papel semântico → índice ANSI (0-15), ex: `{ "destaque": 4, "erro": 1, "texto-secundario": 8, ... }` — nunca valores RGB/hex arbitrários.

## interfaces (TUI)

outro ponto extenso do taskthing é a utilização de TUI para facilitar a edição de configurações e temas mas também para realizar interagir com o usuário de diversas formas essa subsessão irá detalhar como alguns comandos irão funcionar e como a alteração de configurações e temas vai funcionar.

O primeiro fundamento geral é que a propria TUI que mostra as configurações é configurável com aquele esque ansi colors 16 bits.
O segundo ponto fundamento é que usaremos para a nossa ui a lib ink: https://github.com/vadimdemedes/ink

### 1. comandos: install, update check, update apply, sync, rebuild, pull e truncate.

esses comandos são naturalmente assincronos e o usuário precisa ter algum feedback do que está acontecendo por trás. a interface deverá renderizar um spinner da seguinte forma, lembrando que ao final taskthing deve mostrar um status de erro ou sucesso:

Non-NerdFont support:

⣾ installing taskthing...
✓ taskthing installed!
𐄂 something went wrong during taskthing installation. error: <resumed error message>

⣾ looking for updates...
✓ you're on the latest version!
✓ there's a pending update <current_version> → <target-version>!
𐄂 something went wrong during update check. error: <resumed error message>

⣾ updating taskthing...
✓ taskthing updated to version <target-version>
𐄂 something went wrong during taskthing update. error: <resumed error message>

⣾ syncing <workspace-name>...
✓ <workspace-name> synchronized!
𐄂 something went wrong during syncing. error: <resumed error message>

⣾ rebuilding <workspace-name>...
✓ <workspace-name> rebuilt!
𐄂 something went wrong during rebuild. error: <resumed error message>

⣾ pulling <workspace-name>...
✓ <workspace-name> pulled!
𐄂 something went wrong during pull. error: <resumed error message>

⣾ truncating branch <branch-name> from <workspace-name>...
✓ <workspace-name> truncated!
𐄂 something went wrong during truncating. error: <resumed error message>

NerdFont support:

⣾ installing taskthing...
 taskthing installed!
 something went wrong during taskthing installation. error: <resumed error message>

⣾ looking for updates...
 you're on the latest version!
 there's a pending update <current_version> → <target-version>!
 something went wrong during update check. error: <resumed error message>

⣾ updating taskthing...
 taskthing updated to version <target-version>
 something went wrong during taskthing update. error: <resumed error message>

⣾ syncing <workspace-name>...
 <workspace-name> synchronized!
 something went wrong during syncing. error: <resumed error message>

⣾ rebuilding <workspace-name>...
 <workspace-name> rebuilt!
 something went wrong during rebuild. error: <resumed error message>

⣾ pulling <workspace-name>...
 <workspace-name> pulled!
 something went wrong during pull. error: <resumed error message>

⣾ truncating branch <branch-name> from <workspace-name>...
 <workspace-name> truncated!
 something went wrong during truncating. error: <resumed error message>

### 2. comandos de listagem

#### tasks e boards

Definições:

<title> = texto "Tasks" com um backgroun e em negrito um título pequeno porém com personalidade.

<board> = força um underline e negrito em todo o texto dentro inclusive pela cor definida pelo board
<board-icon> = o ícone do board
<board-name> = o nome do board

No caso do inbox que é um board virtual ele terá o ícone:

- Com suporte a nerd font:  com uma cor de destaque diferente definida em código (papel `inbox-accent`, mesmo mecanismo papel→índice ANSI do resto do tema, com índice padrão hardcoded — não é uma exceção com RGB)
- Sem suporte a nerd font: 📬 com uma cor de destque diferente definida em código (mesmo papel `inbox-accent`)

<check-mark> = Sem suporte a nerd font: [ ] e [x]. Com suporte a nerd font:  a box checada deverá ter uma cor de sucesso e  com cor secundária.
<date-time> = um texto colorido e underline, cor a depender da data definido da seguinte forma:

- Data anteriores a ontem = "N days ago"..."2 days ago" terá uma cor escura de perigo.
- Ontem = yesterday, cor de perigo mais claro.
- Hoje = today, cor de destaque outstanding amarelo
- Amanhã = tomorrow, cor de detaque outsting azul
- Datas adiante no mesmo ano atual = <date> <month> ex: "21 aug". cor secundária com pouca importância
- Datas até um ano, que passam do ano atual = <month> <year>. cor secundária com pouca importância
- Datas com mais de um ano = <year>. cor secundária com pouca importância

assimetria intencional: passado usa contagem relativa ("N days ago") pra dar noção de atraso; futuro (além de "tomorrow") usa data absoluta, não "in N days", já que o objetivo do futuro é facilitar planejamento (saber a data exata), não medir distância.
  date-time não deverá ser renderizado de forma tradicional quando a tarefa estiver completa, ao qual deverá ser mostrada um texto pleno de cor secundária com underline (cinza) a data completa de completação respeitando o formato escolhido pelo usuário.
  <star> = ⭐ para setups sem nerdfont e 󰩳 amarelo com suporte a nerdfonts, caso a tarefa não tenha star simplesmente não renderiza nada.
  <task-title> = titulo da tarefa em texto pleno
  <description> = destaca que a task tem uma descrição através dos símbolos 📄 sem suporte a nerd-font e 󰆈 (U+F0188) para nerd-font, com cor secundária; caso a tarefa não tenha descrição não renderiza nada.
  <recurring> = destaca que a task é recorrente através dos simbolos que são 🔄 sem suporte a nerd-font e  para nerd-font com uma cor de destaque diferente das demais

Renderização:

<title>

<board><board-icon> <board-name></board>

1. <check-mark> <date-time> <star> <title> <description> <recurring>
   ...
   N. <check-mark> <date-time> <star> <title> <description> <recurring>

Como da para perceber os números estão diretamente ligados ao kv-store.

#### boards

Definições:

<title> = texto "Boards" com um backgroun e em negrito um título pequeno porém com personalidade.

<board> = força um underline e negrito em todo o texto dentro inclusive pela cor definida pelo board
<board-icon> = o ícone do board
<board-name> = o nome do board

No caso do inbox que é um board virtual ele terá o ícone:

- Com suporte a nerd font:  com uma cor de destaque diferente definida em código (papel `inbox-accent`, mesmo mecanismo papel→índice ANSI do resto do tema)
- Sem suporte a nerd font: 📬 com uma cor de destque diferente definida em código (mesmo papel `inbox-accent`)

Renderização:

<title>

1. <board><board-icon> <board-name></board>
   ...
   N. <board><board-icon> <board-name></board>

#### migrations

Definições:

<title> = texto "Migrations" com um background e em negrito um título pequeno porém com personalidade.

<version> = a versão da migração
<applied?> = se a migração foi aplicada ou não <yes|no>
<validation-message> =
Com suporte a NF:

 this workspace has no migrations peding
 there's peding migrations. verify our taskthing installation.

Sem suporte a migração:

ℹ️ this workspace has no migrations peding
⚠️ there's peding migrations. verify our taskthing installation.

Renderização

<title>

1. <version> <applied?>
   ...
   N. <version> <applied?>

<validation-message>

### 3. comandos de edição específicos

Vamos lá até agora foram definidos padrões e tudo mais agora vamos para as interfaces de edição, confirmação e etc. Lembrando mais uma vez que: os temas também impactam diretamente nessas UIs:

#### UI de confirmação

O primeiro caso de uso é em relação ao adicionar o remote em um workspace especialmente no caso 1.3 e 1.2 que obrigam o usuário a "limpar o workspace" antes então deve-se ter uma ui de confirmação nesse caso.

O segundo caso de uso acontece caso o usuário está tentando atualizar uma task com rrule, ao final ele deve ser confirmado. o `<question>` mostra o resultado já interpretado pelo parser (data natural via chrono + rrule), não uma confirmação genérica — ex: `"every monday, starting tomorrow (23 jul) — confirm?"` — pra o usuário conseguir pegar erro de parsing antes de salvar.

O terceiro caso de uso é `taskthing workspace delete <name>` — operação destrutiva (apaga a pasta local), sempre confirmada antes de executar.

Definição

<title> = a title for the form, again small title but outstanding
<selector> = sem suporte a nerdfont (→) com suporte a nerd font (),
<question> = texto de cor primaria seguido de um ponto de interrogação ?
<yes/no> = input de texto que deve aceitar y ou n yes or no enfim padrão. deve haver um placeholder

Renderização

<title>

<selector> <question> (y/n) ? <yes/no>

#### UI de seleção

Caso de uso dessa UI deverá ser para opções finitas como o formato de data do usuário (american (yyyy/mm/dd)) e (europe (dd/mm/yyyy))

Esse UI deverá ser possível ser mono select ou multi-select sendo o resultado dessa operação sempre um array pois no caso do mono o array será de 1 objeto. (multi-select é especificação especulativa por enquanto — nenhum comando do doc atual a usa; formato de data, nerdfont e tema são todos mono/confirmação. Mantido no design pra uma necessidade futura ainda não identificada.)

Definição

<title> = a title for the form, again small title but outstanding
<selector> = sem suporte a nerdfont (→) com suporte a nerd font (), deve ser só renderizado para o item atual
<seleceted?> = sem suporte a nerdfont (○) para o item não selecionado e (●) para o item selecionado. para o suporte a nerd font () para item não selecionado e () para selecionado
<item-name> = o nome do item em texto pleno, mudando de cor caso seja o selecionado atualmente.

Renderização

<title>

<selector> <selected> <item-name>
...
<selector> <selected> <item-name>
