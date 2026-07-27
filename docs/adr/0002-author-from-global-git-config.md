# Autoria de mudanças vem da configuração global do git

Todo campo controlado por LWW carrega o nome de quem fez a última mudança, mesmo em workspaces locais (sem repositório git). Consideramos usar uma identidade própria do taskthing (configurada em `config.md`), mas decidimos reaproveitar o `user.name` da configuração global do git da máquina — mesmo para workspaces locais que não são repositórios git. Isso implica que taskthing depende de o usuário ter o git instalado e configurado globalmente, mesmo para uso puramente local sem nenhum remoto.
