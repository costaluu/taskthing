# Self-update no Windows renomeia o exe em vez de sobrescrever

O `BinaryFs.swap` (Spec 0005) grava o binário baixado ao lado do atual (`<exe>.new`) e move-o por cima
via `rename`. Em Linux/macOS isso funciona mesmo com o processo rodando: `rename` sobre um arquivo aberto
apenas troca a entrada de diretório, o inode antigo permanece válido para quem ainda o tem aberto.

No Windows, o mesmo `rename(tmp, target)` falha com `EPERM`: sobrescrever um arquivo já existente exige
acesso de exclusão sobre o **destino**, e o `.exe` em execução — o próprio `taskthing update apply` rodando
— está bloqueado contra exclusão/substituição nesse modo. Isso foi reproduzido rodando `update apply` real
(v0.0.1 → v0.0.2 local): o download completa, mas o swap final quebra, e o `apply` propaga o erro.

Decidimos que, apenas no Windows (`process.platform === "win32"`), o `swap` faça a troca em duas etapas:

1. `rename(target, target + ".old")` — renomear (não sobrescrever) o exe em execução é permitido pelo
   Windows; libera o nome original sem exigir exclusão do arquivo bloqueado.
2. `rename(tmp, target)` — mover o binário novo para o nome agora livre é uma criação, não uma
   substituição, então não esbarra no lock.

O `target + ".old"` fica órfão até que o processo que ainda o executa termine e libere o lock; até lá,
`rm` sobre ele falha silenciosamente (best effort, `.catch(() => {})`). Para não depender de o usuário
notar e apagar manualmente, `Updater.apply` chama `BinaryFs.cleanupStale?()` **no início de toda
execução**, antes mesmo de decidir se há uma atualização pendente — não só quando um swap de fato ocorre.
Assim um `.old` deixado por um `apply` anterior é varrido na próxima chamada, mesmo que essa chamada só
reporte "up-to-date".

Em Linux/macOS, `cleanupStale` é um no-op: o `rename` de sobrescrita já funciona em um passo, não há
arquivo `.old` a limpar.

Alternativas consideradas e rejeitadas:

- **Processo auxiliar destacado** que espera o processo atual encerrar antes de renomear: resolve o mesmo
  problema, mas introduz um processo desacoplado, um script/binário auxiliar e limpeza dele próprio — custo
  desproporcional ao problema (um `rename` extra resolve).
- **Adiar o swap para a próxima inicialização** (checar por um `.new` órfão no boot de qualquer comando):
  exigiria um hook em todo ponto de entrada do CLI e faria o `apply` reportar sucesso antes do binário
  realmente ter trocado — pior UX que a solução de duas renomeações, que já troca no mesmo processo.
- **`MOVEFILE_DELAY_UNTIL_REBOOT`** (API do Windows para agendar a substituição no próximo boot): exigiria
  reiniciar a máquina para um CLI, inaceitável.

Isto não contradiz a [ADR-0005](./0005-ansi-only-no-named-presets.md) (não relacionada) nem a Spec 0005
story 16 ("download-then-replace, uma falha de download nunca deixa um binário pela metade"): a ordem
continua download completo → só então qualquer rename; a mudança é só em quantos renames o passo final
leva no Windows.
