
Bonne question — il y a un vrai fossé entre "ça marche sur ma machine avec 3 processus" et "complètement fonctionnel en cluster". Le point le plus important d'abord, puis le reste.

Le vrai bloquant : rbac.json n'est pas distribué
Chaque nœud stocke la même donnée queel (textes/rounds/fragments/votes) via cluster.DistributedStore — ça, c'est réellement clusterisé. Mais les droits rbac vivent dans un simple fichier JSON réécrit en entier à chaque modification (rbac.Store.persist()), sans verrou ni coordination entre processus. Ça ne fonctionne que parce que mes 3 nœuds de test tournent sur la même machine et pointent vers le même fichier sur le même disque. Sur plusieurs machines, ça casserait immédiatement (chaque nœud aurait sa propre copie divergente, ou pire, des écritures concurrentes corrompraient le fichier).

La vraie correction : migrer rbac vers Postgres (déjà une infra partagée, avec de vraies transactions) plutôt que de le répliquer via le mécanisme cluster de queel. C'est le changement le plus important pour que "mode cluster" soit honnête sur plusieurs machines.

Le reste, par ordre d'impact
Config pure (pas de code à changer) pour un vrai déploiement multi-machines :

QUEEL_NODE_ADDRESS/QUEEL_SEED_NODE en adresses réseau réelles (pas localhost).
Pare-feu : le port interne (réplication/gossip) accessible uniquement entre nœuds du cluster, jamais public — ces routes ne sont pas authentifiées par design.
Un load-balancer devant les N ports publics de l'API.
Un volume persistant par nœud pour QUEEL_DATA_DIR.
Nécessite du code pour être vraiment complet :

rbac → Postgres (ci-dessus, le plus important).
TLS/mTLS entre nœuds si le réseau interne n'est pas déjà de confiance (VPC privé) — actuellement tout le trafic de réplication est en clair et sans auth.
Endpoints de santé (/healthz) pour l'orchestration (Kubernetes, etc.).
Anti-entropie en arrière-plan : aujourd'hui un nœud qui a perdu des données ne se resynchronise que paresseusement, clé par clé, au moment d'une lecture (read-repair) — pas de resynchronisation complète proactive après une perte de disque.
Décommissionnement propre d'un nœud : un nœud mort est juste ignoré par le ring une fois détecté ; rien ne rééquilibre proactivement ses données vers les autres.
Tu veux que je m'attaque à la migration rbac → Postgres en premier ? C'est le seul point qui rend le mode cluster actuellement "faux" dès qu'on sort d'une seule machine — les autres sont plutôt du durcissement production.