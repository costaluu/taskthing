# Plano: ids que começam com `-` e sugestão de emoji para boards

Escrito em 2026-11-14, na branch `autocomplete` (HEAD `afc2b76`). Frente 1 implementada na branch `dash-ids` em 2026-11-14. **Atualização 2026-11-18:** as frentes 2 e 3 foram substituídas por um seletor de emoji interativo (`internal/emoji` + `internal/app/emojipicker.go`, branch `emoji-picker`): busca por nome e palavras-chave do CLDR em inglês, sem embedding e sem sugestão automática. As seções 2 e 3 abaixo ficam como registro do estudo. São três frentes independentes, na ordem em que eu as faria:

1. Ids que começam com `-` (corrige um defeito real e um teste instável).
2. Sugestão de emoji por palavras-chave do Unicode (CLDR).
3. Embedding estático gerado fora do build, sem inferência no binário.

Cada frente lista fatos conferidos, decisões que dependem de você, os ciclos de TDD e como verificar. O que não foi conferido está marcado como "a confirmar".

---

## 1. Ids que começam com `-`

### Fatos

- O alfabeto do nanoid inclui `-` e `_` (`internal/mdwal/mdwal.go:99`). Um id em 64 começa com `-`.
- `parseArgs` (`internal/app/cli.go:306`) trata qualquer argumento que começa com `-`, e não é número negativo, como opção curta. Em modo estrito, opção desconhecida é erro.
- Medido: `TestBoardDeleteRecover` falhou 6 vezes em 300 execuções, sempre com `Unknown option '-<id>'`. O teste passa o id cru para `board recover`.
- Onde um id cru chega à linha de comando:
  - `resolveTask`: toda referência que não é número inteiro é lida como id (`internal/app/porcelain.go`).
  - `resolveBoard`: cai em `findByID` quando não é `inbox`, número nem nome.
  - O TAB de `board recover` oferece boards deletados **pelo id** (`boardCandidates`, `internal/app/complete.go`), porque o nome de um board deletado não resolve mais.
- No completion, uma palavra atual que começa com `-` vai direto para o ramo de flags (`complete.go`, `strings.HasPrefix(cur, "-")`), então um id com `-` nunca é completado.
- Hoje o único jeito de usar um id desses é `--` antes dele.
- Ids com `-` já existem em workspaces reais e continuarão chegando de peers com binários antigos. Corrigir só o gerador não resolve esses.

### Opções

| Opção | Resolve ids novos | Resolve ids existentes e de peers | Custo |
| --- | --- | --- | --- |
| A. Gerador nunca produz id começando com `-` | Sim | Não | Uma condição em `newID` |
| B. Parser aceita como posicional um argumento com forma de id | Sim | Sim | Um typo de opção com exatamente essa forma vira "no such task" em vez de "unknown option" |
| C. Só trocar o teste para usar `--` | Não | Não | Esconde o defeito |

**Recomendação: A e B juntas.** A impede o caso de nascer; B cobre o que já existe. Em B, a regra seria estreita: o argumento tem exatamente 21 caracteres do alfabeto do nanoid, começa com um único `-` e não casa com nenhuma opção declarada do comando. Ids que começam com `--` (1 em 4096) ficam de fora de B, porque colidiriam com typos de opções longas; A acaba com os novos, e os antigos seguem exigindo `--`.

**Decisão sua:** aceitar a troca de mensagem de erro descrita em B.

### Ciclos de TDD

1. **Gerador** (teste em `internal/mdwal/mdwal_test.go`, pela API pública). `Config.IDs` já permite injetar o gerador. Teste: um gerador que devolve `-aaaaaaaaaaaaaaaaaaaa` e depois `baaaaaaaaaaaaaaaaaaaa`; `CreateEntity` deve usar o segundo. Implementação: ampliar o laço de `newID`, que hoje só rejeita `inbox`.
2. **Reprodução determinística no CLI** (`cmd/taskthing/cli_test.go`). Criar board e tarefa com id fixo começando com `-`, usando o mesmo `Config.IDs` no helper de seed. Tabela com todo verbo que recebe referência: `check`, `uncheck`, `star`, `unstar`, `delete`, `recover`, `clear`, `set title|description|date-time|board`, `board delete|clear|recover|switch`, `board set name|icon|color`. Cada um deve sair com código 0 recebendo o id cru. Vermelho hoje; verde com a regra B em `parseArgs`.
3. **Typos continuam sendo erro.** `check -x`, `list -zz` e uma opção curta desconhecida de 21 caracteres fora do alfabeto seguem recusados com "Unknown option". Este teste protege o custo de B.
4. **Completion.** `__complete board recover -Ab` deve oferecer o board deletado cujo id começa assim; `__complete check -` continua oferecendo flags. Implementação: no ramo de flags, somar os candidatos de `cmd.Args` que começam com a palavra atual.
5. **Shell real** (um caso em `TestShellsCompleteAddText`, bash e zsh): TAB completa o id com `-` e o comando resultante roda.

### Verificação

- `go test ./cmd/taskthing/ -count=500 -run TestBoardDeleteRecover`: zero falhas (hoje, cerca de 10 em 500).
- Suíte inteira verde.

### Fora do escopo

Renomear ids existentes. O id é o nome do arquivo e aparece nos eventos de todos os peers.

---

## 2. Sugestão de emoji por palavras-chave do Unicode

### Fatos

- Fonte: pacote `cldr-annotations-full` do repositório `unicode-org/cldr-json`, versão 49.0.0, licença `Unicode-3.0`.
- Formato de `annotations/en/annotations.json`: `annotations.annotations` mapeia cada emoji para `{"default": [palavras-chave], "tts": [nome]}`. Exemplos conferidos:
  - 🛒: `cart`, `shopping`, `trolley`; nome `shopping cart`.
  - 💼: `briefcase`, `office`; nome `briefcase`.
  - 📚: `book`, `books`, `education`, `fantasy`, `knowledge`, `library`, `novels`, `reading`, `school`, `study`; nome `books`.
  - 💰: 16 palavras, entre elas `bank`, `cash`, `money`, `rich`; nome `money bag`.
- O arquivo inteiro tem cerca de 436 mil caracteres e inclui símbolos que não são emoji (`{`, `+`).
- `emoji-test.txt` (Unicode Emoji 18.0, de 2026-04-30) define a lista oficial, a ordem, os grupos e o status de cada sequência (`fully-qualified`, `component` etc.). As contagens totais estão a confirmar.
- O ícone do board é uma string livre (`internal/mdwal/schema.go:228`), desenhada como está, com ou sem nerdfont (`internal/app/present.go:213`). Não há restrição técnica a emoji.
- O projeto tem quatro dependências (`go.mod`). Esta frente não precisa de nenhuma nova.

### Decisões suas

1. **Idioma dos nomes de board.** A CLI é em inglês, mas o nome do board é dado seu, e você escreve em português. O CLDR tem anotações em português no mesmo formato. Proponho embutir inglês e português. Isso dobra a tabela e é o que faz "trabalho" e "casa" funcionarem.
2. **Quais emojis entram.** Proponho: só `fully-qualified`, sem modificadores de tom de pele e sem sequências ZWJ (famílias, profissões com gênero), que têm largura inconsistente entre terminais. Bandeiras entram.
3. **Aplicar sozinho ou só sugerir.** Ao criar um board sem `--icon`, o taskthing pode (a) só mostrar ideias na linha de resultado, ou (b) aplicar a melhor e avisar. Recomendo (a): o ícone fica visível em toda listagem, e um palpite errado incomoda mais do que ajuda. Com `undo`, (b) também é reversível.

### Desenho

- **Pacote novo `internal/emoji`**, sem dependência do resto do app. Uma função pura: `Suggest(texto, k)` devolve os `k` melhores, cada um com emoji e nome.
- **Dados embutidos, gerados fora do build.** Um gerador em Go (`internal/emoji/gen`, chamado por `go generate`) lê os arquivos do CLDR e o `emoji-test.txt` guardados no repositório, com versão e sha256 anotados, e escreve uma tabela compacta (`emoji<TAB>nome<TAB>palavras`) na ordem do `emoji-test.txt`. O `go build` nunca acessa a rede. Tamanho esperado: algo entre 100 e 300 KB por idioma, a medir.
- **Pontuação**, determinística:
  1. Normalizar o texto: minúsculas, separar em palavras, remover acentos para comparar, plural simples.
  2. Por palavra do texto: nome exato do emoji vale mais; palavra-chave exata, um pouco menos; palavra do nome, menos; prefixo de palavra-chave com 3 letras ou mais, menos ainda.
  3. Somar por emoji; desempate pela ordem oficial.
  Os pesos exatos saem do conjunto de avaliação abaixo, não de palpite.
- **Aviso de licença** do Unicode num arquivo `NOTICE`.

### Onde aparece

1. **TAB no ícone.** `board add "Groceries" --icon <TAB>` e `board set icon <board> <TAB>` listam os melhores emojis para o nome do board, com o nome de cada um como descrição. Se você digitar texto (`--icon rock<TAB>`), a busca usa o texto digitado. A ordem do binário já é preservada nos dois shells.
2. **Linha de resultado do `board add`** sem `--icon`, conforme a decisão 3.

### Conjunto de avaliação

Antes de ajustar pesos: um arquivo `testdata/emoji/board-names.tsv` com cerca de 60 nomes de board reais, em inglês e português, e os emojis aceitáveis para cada um. **Preciso que você revise ou escreva essa lista**, porque ela define o que é "acertar". Métrica: fração de nomes com um emoji aceitável entre os 5 primeiros. O número do CLDR sozinho vira a linha de base da frente 3.

### Ciclos de TDD

1. **Gerador:** um JSON pequeno de exemplo e um `emoji-test.txt` reduzido produzem a tabela esperada, escrita à mão no teste. Cobre os filtros da decisão 2.
2. **`Suggest`:** casos tirados dos fatos acima: `shopping` traz 🛒 em primeiro, `office` traz 💼, `books study` traz 📚, `money` traz 💰 entre os três primeiros; texto sem correspondência devolve vazio.
3. **Avaliação:** um teste lê `board-names.tsv` e falha se o acerto cair abaixo do valor registrado.
4. **Completion** (`__complete`, caixa-preta): os dois casos do item "TAB no ícone".
5. **Shell real:** um caso em bash e zsh, para garantir que o emoji entra na linha inteiro.
6. **Linha de resultado** do `board add`, conforme a decisão 3.

### Limite conhecido

Sinônimos fora das palavras-chave. "groceries" não casa com 🛒, cujas palavras são `cart`, `shopping` e `trolley`. É o que a frente 3 ataca.

---

## 3. Embedding estático gerado fora do build

### O que "sem inferência" quer dizer aqui

Nenhum transformer roda no binário. Em tempo de execução sobram quatro passos baratos: tokenizar, buscar o vetor de cada token numa tabela, tirar a média e comparar com os vetores dos emojis, já calculados. O transformer só roda uma vez, numa máquina de desenvolvimento, para produzir as tabelas.

### Fatos

- **Model2Vec** (MinishLab) faz exatamente isso: passa um vocabulário por um sentence transformer e guarda um vetor por token. API de destilação:
  ```python
  from model2vec.distill import distill
  m2v_model = distill(model_name="BAAI/bge-base-en-v1.5")
  m2v_model.save_pretrained("m2v_model")
  ```
  Parâmetros conferidos na documentação: `model_name` ("Any SentenceTransformer-compatible model works"), `vocabulary`, `pca_dims` (padrão 256), `sif_coefficient` (padrão `1e-4`), `quantize_to` (padrão `float16`), `vocabulary_quantization`, `pooling` (padrão `mean`). Os arquivos que `save_pretrained` grava estão a confirmar.
- **O modelo que você indicou** (`SmartComponents/bge-micro-v2`) pode ser o professor da destilação. O repositório tem `pytorch_model.bin` de 34,8 MB, `vocab.txt` de 232 kB e `tokenizer.json` de 712 kB. A licença é fraca na origem: o arquivo `LICENSE` aponta para uma discussão em que o mantenedor responde "so mit i guess".
- **Alternativa pronta:** `minishlab/potion-base-2M`, licença MIT, 1,89 M de parâmetros, destilado do `bge-base-en-v1.5`. Seu `config.json`: `hidden_dim` 64, `apply_pca` 64, `apply_zipf` true, `normalize` true, tokenizer `baai/bge-base-en-v1.5`.
- **Tamanho** de 1,89 M de parâmetros: 7,6 MB em float32, 3,8 MB em float16, 1,9 MB em int8. O binário instalado hoje tem cerca de 8 MB.
- **Go puro:** a busca encontrou o pacote `github.com/townsendmerino/aikit`, que declara fazer inferência de Model2Vec sem cgo (tokenizer WordPiece, leitor de safetensors, normalização L2). Não li o código nem a licença: a confirmar. A alternativa é escrever esses passos no projeto, que são poucos.
- Os dois modelos acima são **só de inglês**.

### Duas variantes

| | V1: tabela palavra → emojis | V2: vetores de token no binário |
| --- | --- | --- |
| O que vai no binário | Para cada palavra de um vocabulário, os melhores emojis já calculados | Matriz de tokens, vocabulário e matriz de emojis |
| Código novo em Go | Quase nenhum: é mais uma tabela para a frente 2 | Tokenizer WordPiece, média, produto escalar |
| Palavra fora do vocabulário | Sem resposta | Responde, pelos pedaços da palavra |
| Texto com várias palavras | Soma de votos por palavra | Média dos vetores, que captura melhor a combinação |
| Tamanho esperado | Abaixo de 1 MB | 2 a 4 MB com int8 |

**Recomendação:** fazer primeiro um experimento curto com as duas variantes, medir no conjunto de avaliação da frente 2 e só então escolher. Critério proposto: V2 só entra se acertar pelo menos 15 pontos percentuais a mais que o CLDR sozinho; se V1 chegar perto de V2, fica V1.

### Decisões suas

1. **Idioma.** É a decisão que mais pesa. Com nomes de board em português, um professor só de inglês vai mal. Caminhos: um professor multilíngue (a pesquisar; nenhum foi conferido) ou aceitar semântica só em inglês e deixar o português por conta das palavras-chave do CLDR.
2. **Teto de tamanho** que você aceita somar ao binário.
3. **Dependência nova** (`aikit`) ou código próprio.
4. **Professor:** `bge-micro-v2` (sua escolha, licença de origem fraca) ou partir do `potion-base-2M` pronto (MIT).

### Pipeline, fora do `go build`

1. **Script** em `tools/emoji-embed/`, em Python, rodado no container `agent-dev`, com versões fixadas. Nunca roda no `go build` nem no CI de release.
2. **Entrada:** a tabela da frente 2. O texto embutido de cada emoji é o nome seguido das palavras-chave.
3. **Saída**, commitada em `internal/emoji/data/`:
   - matriz de tokens quantizada em int8, com a escala;
   - vocabulário do tokenizer;
   - matriz de vetores dos emojis, na mesma ordem da tabela;
   - um manifesto com sha256 de cada arquivo, revisão do modelo, versão do CLDR e versão do script.
4. **Os arquivos commitados são a fonte de verdade.** A redução por PCA pode variar entre máquinas, então o build não tenta regenerá-los. Um teste em Go confere os sha256 do manifesto.
5. **Paridade:** o script também grava cerca de 50 textos com o vetor e os 5 melhores emojis calculados em Python. É a fonte independente contra a qual o Go é testado.

### Ciclos de TDD (V2)

1. **Tokenizer:** textos com os ids produzidos pelo tokenizer original, gravados pelo script. Inclui acentos, maiúsculas e palavra fora do vocabulário.
2. **Vetor do texto:** cosseno de pelo menos 0,99 contra o vetor do Python, com a tolerância da quantização.
3. **Ranking:** o melhor emoji coincide com o do Python nos textos de paridade.
4. **Combinação com a frente 2:** correspondência exata do CLDR vem primeiro; o embedding completa a lista. Teste: `shopping` segue trazendo 🛒 em primeiro, e `groceries` passa a trazê-lo entre os cinco.
5. **Avaliação:** o acerto registrado sobe pelo valor decidido, senão a frente não entra.
6. **Custo:** `board add --icon <TAB>` soma menos de 5 ms; comandos que não tocam em ícone não mudam de latência. As tabelas só são lidas nesses caminhos.

Para V1, os ciclos 1 a 3 somem, e entra um teste do formato da tabela.

### Riscos

- **Qualidade:** embeddings estáticos perdem contexto. Pode ser que não ganhem do CLDR em nomes curtos. O experimento existe para descobrir isso antes de pagar o custo.
- **Tamanho do binário**, em um app que hoje tem 8 MB.
- **Licença** do `bge-micro-v2`.
- **Quantização em int8** pode custar acerto; medir float16 contra int8 no experimento.

---

## Ordem sugerida

1. Frente 1 inteira, que deixa a suíte estável.
2. Frente 2, ciclos 1 a 3, mais o conjunto de avaliação revisado por você.
3. Frente 2, ciclos 4 a 6.
4. Experimento da frente 3 e decisão.
5. Frente 3, se o experimento justificar.

## Fontes

- CLDR: <https://github.com/unicode-org/cldr-json> (`cldr-json/cldr-annotations-full`)
- Lista de emoji: <https://unicode.org/Public/emoji/latest/emoji-test.txt>
- Model2Vec: <https://github.com/MinishLab/model2vec>, <https://minish.ai/packages/model2vec/distillation>, <https://minish.ai/packages/model2vec/inference>
- `potion-base-2M`: <https://huggingface.co/minishlab/potion-base-2M>
- `bge-micro-v2`: <https://huggingface.co/SmartComponents/bge-micro-v2>
- `aikit`: <https://pkg.go.dev/github.com/townsendmerino/aikit>
