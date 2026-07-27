# Timestamps de evento são monotônicos por peer

O purge do `sync` 2.2 apaga os eventos do próprio peer com `ts <= note[peer]`, onde `note[peer]` é o maior timestamp que o último `rebuild` consolidou na `master`. Usar o timestamp de parede cru (`Date.now()` em UTC) como critério reintroduz perda silenciosa de dados: se o relógio UTC anda pra trás (NTP em step, restore de snapshot de VM, suspend-resume, acerto manual) *depois* de um rebuild, um evento novo pode nascer com `ts < note[peer]` e ser purgado antes de jamais ter sido consolidado por qualquer peer. UTC não protege — o problema não é fuso, é a não-monotonicidade do relógio físico.

Decidimos gerar todo timestamp de evento como `ts = max(now_utc_ns, último_ts_local_ns + 1)`, tornando o `events.log` de cada peer estritamente crescente. Com isso o purge por timestamp fica provavelmente correto (o threshold por timestamp passa a coincidir com a fronteira do prefixo consolidado, e todo evento pós-rebuild tem `ts > note[peer]` por construção), e some a auto-inconsistência de LWW entre edições sucessivas do mesmo peer.

Consideramos purgar por **posição no log** (offset/contagem do prefixo consolidado) em vez de por timestamp, o que também fecharia o furo, mas preferimos manter o critério de purge homogêneo com o resto do domínio (tudo é decidido por timestamp) e resolver a causa-raiz na geração do timestamp.

Isto **não** contradiz a [ADR-0001](./0001-lww-plain-timestamp-no-hlc.md): não é um Hybrid Logical Clock. Não há contador lógico nem troca de causalidade entre peers — é apenas um *guard* de monotonicidade local sobre o relógio físico. A comparação de LWW entre peers permanece timestamp-vs-timestamp puro, com desempate arbitrário.
