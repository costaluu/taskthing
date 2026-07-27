Standards
Hard violations

Dependência nova não-usada — react-devtools-core (package.json:18, bun.lock). Adicionada como dependência de runtime mas importada em lugar nenhum no repo. Viola o working-agreement do CLAUDE.md "no new dependencies without reason". Pior: vendora transitivamente ws@7.5.13 e shell-quote — e o CLAUDE.md diz explicitamente "WebSocket is built-in. Don't use ws." Recomenda remover.
Baseline smells (judgement calls)

Duplicated Code — helper frameShowing copiado verbatim em spinner.test.tsx e command-spinner.test.tsx (mesmo loop de poll 200×5ms, mesma assinatura). Extrair pra um helper de teste compartilhado.

Duplicated Code — scaffold de workspace remoto repetido em cada caso novo de cli.test.ts (:108-133, :135-170, :172-203): mesmo mkdtemp remote → git init --bare → createManager({...author:()=>"alice",clock:()=>1n}) → manager.init(remote) → rm/rm no finally. Dobrar num fixture.

Primitive Obsession / re-parse — dispatch do entity (cli.ts:438-447). op é string cru num switch em authorEntityLog, e args são parseados duas vezes (parseFlags(args).has("log") depois args.filter(...)). Menor.

Conforme / limpo

Uso de Bun correto em todo lugar (Bun.file().text(), Bun.write, Bun.$, Bun.semver.order). node:fs/promises fica no conjunto permitido (mkdtemp/rm/mkdir/readdir/rename) — mkdtemp/tmpdir novos em cli.ts são legítimos (Bun não tem).
Refactor de migration-runner.ts (applied → appliedIn(dir), failures[].root → .where) é generalização limpa com dois call sites reais, não Speculative Generality.
Sem dotenv/express/execa. Install scripts e release.yml são shell/CI, fora das convenções TS do CLAUDE.md; nada objetável.
Spec
Li o diff completo, a spec, os dois ADRs e os caminhos de código ligados. No geral o change segue a spec de perto.

Verificado correto (áreas suspeitas do brief):

Nomes de asset casam com o updater. Workflow emite taskthing-<linux|darwin|win32>-<x64|arm64>[.exe]; selectAsset (updater.ts:88) casa name.includes(platform.os) && includes(platform.arch) — sem colisão.
Sed do version-stamp (release.yml:35) casa export const VERSION = "0.1.0" e tira o v do tag. Sólido.
Version barrier dispara no replay para pull/sync/rebuild (manager.ts:163,115 e 411 → migrateEvent lança em event > currentVersion, mdwal.ts:335). As três commands da story 37 cobertas, mesmo só pull/rebuild tendo teste explícito.
Config migration idempotente, registrada uma vez por máquina em <configHome>/migrations/, all-or-nothing na escrita, não-registrada na falha (migration-runner.ts:117-127) — casa stories 31/34.
Parcial / desvio:

Comando de autoria config ausente; edições de config não podem viver num log de migração estrutural. Story 20: "a structural migration expressed as a static mdwal log authored with plumber commands (entity …, config …, folder ops, all with --log)" e story 31 quer migração capaz de "add/remove/rename config fields". Mas applyEvent (migration-runner.ts:74) rejeita qualquer op não-folder, e nenhum comando plumber config foi adicionado (só entity). Mudanças de config são alcançáveis só via o caminho separado CONFIG_MIGRATIONS. Consistente com a realização das Implementation-Decisions ("config.md edits … applied directly, since config is not replayed from a log"), então é um estreitamento defensável — mas a superfície de autoria config … --log da story 20 fica não-implementada, e uma migração estrutural não pode juntar folder-op e edição de config como um log.
Janela record-after-write em migrateConfig: se config.write sucede mas o Bun.write do record falha, o transform re-roda no próximo update — o próprio comentário avisa que um rename re-aplicado "would corrupt config." Baixa severidade, inerente à ordenação record-after-apply da spec.
Sem scope creep preocupante. entity, o fallback plain-print não-TTY, e a dep react-devtools-core servem stories declaradas (20, 6/34, e o requisito de compile).

Resumo
Standards: 1 violação hard (dep react-devtools-core não-usada puxa ws, que o CLAUDE.md proíbe) + 3 judgement calls (2× Duplicated Code, 1× Primitive Obsession). Pior: a dep nova.
Spec: 0 faltantes críticos, 2 desvios (superfície de autoria config …--log da story 20 não-implementada — narrowing defensável; janela record-after-write de baixa severidade). Pior: a story 20 config …--log ausente.
Contexto que os agentes não tinham, sobre a pior finding de Standards: o react-devtools-core não é dead weight acidental — o ink faz import devtools from 'react-devtools-core' estaticamente, e o bun build --compile (o CI release workflow) falha sem ele. Em runtime fica inerte (guardado por DEV==='true'), então o ws transitivo nunca é carregado. Ou seja: a preocupação do CLAUDE.md ("não use ws") é sobre nós usarmos ws no nosso código — aqui é uma dep transitiva de uma lib de UI, não uso direto nosso. Ainda assim, vale (a) documentar o porquê num comentário do package.json/build.ts, ou (b) avaliar mover pra optionalDependencies/trustedDependencies — mas mover corre o risco do CI não instalar. Minha recomendação: manter em dependencies com um comentário explicando que é requisito do --compile, pra não parecer acidental numa próxima revisão.

Quer que eu aplique algum dos fixes (extrair frameShowing compartilhado, dobrar o scaffold de teste num fixture, documentar a dep), ou discutir a story 20 config …--log?
