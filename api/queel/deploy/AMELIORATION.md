
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

## Le principe d'anti-entropie en arrière-plan

**Le problème que ça résout.** Le cluster répare déjà les données de deux façons partielles :
- **Read-repair** (déjà implémenté, voir `Coordinator.Get`/`readRepair` dans `queel/cluster/coordinator.go`) : quand une clé est *lue*, le coordinateur compare les réponses des répliques et pousse la version la plus récente vers celles qui étaient en retard. Ça ne corrige que les clés effectivement lues.
- **Quorum d'écriture** : une écriture réussit dès qu'une majorité des répliques l'a acquittée — la ou les répliques manquantes restent en retard tant que personne ne relit cette clé précise.

Le trou : si un nœud tombe, redémarre avec un disque vide (perte totale), ou rate des écritures pendant une coupure réseau, tout ce qu'il a manqué et que **personne ne relit jamais** reste définitivement sous-répliqué. Le facteur de réplication effectif de ces clés est silencieusement dégradé — invisible jusqu'au jour où un deuxième nœud tombe aussi et qu'une lecture échoue faute de quorum, ou pire, où la donnée est perdue pour de bon.

**Le principe.** Un processus qui tourne en tâche de fond sur chaque nœud, indépendamment de tout trafic client, et dont le seul travail est de comparer périodiquement l'état complet d'un nœud avec celui de ses pairs pour repérer et corriger les divergences — pas seulement les clés qu'on a la chance de relire. C'est l'équivalent, à l'échelle du cluster entier, de ce que `read-repair` fait déjà à l'échelle d'une seule clé.

**Comment ça marche typiquement** (Cassandra, Riak, DynamoDB s'appuient tous là-dessus) :
1. Chaque nœud construit un résumé compact de son contenu — le plus souvent un **arbre de Merkle** (arbre de hachages : chaque feuille hache une plage de clés, chaque nœud parent hache ses enfants) plutôt qu'une liste brute de clés, pour que la comparaison entre deux nœuds coûte `O(log n)` échanges au lieu de transférer tout le dataset.
2. Périodiquement (ex. toutes les heures), un nœud échange son arbre avec un pair et descend récursivement dans les branches dont le hash diffère — les branches identiques sont ignorées d'un coup.
3. Une fois les clés réellement divergentes identifiées, il ne reste qu'à répliquer les versions manquantes ou obsolètes — exactement le même mécanisme que `readRepair` utilise déjà (comparaison de timestamp, on garde la plus récente).

**Dans ce codebase**, ça se raccrocherait naturellement à ce qui existe déjà :
- `cluster.Coordinator.Scan` sait déjà interroger tous les pairs connus et fusionner par timestamp — la brique de fusion existe.
- Il manquerait : (a) un moyen pour un nœud d'énumérer efficacement *tout* son keyspace pour construire l'arbre (aujourd'hui `Engine.Scan` marche par préfixe, pas par plage globale hashable) ; (b) un job périodique (goroutine + ticker, dans le style de `bootstrap.Join`'s boucle de refresh du `Coordinator`) qui déclenche la comparaison avec un pair aléatoire et rejoue les écritures manquantes via `PeerClient.Put`/`PutBatch`.
- Sans arbre de Merkle (plus simple à implémenter, moins efficace réseau), une version dégradée mais déjà utile : scanner périodiquement tout le keyspace local et le comparer clé par clé avec un pair — coûteux sur un gros dataset, mais largement suffisant vu la taille de corpus que `queel` vise aujourd'hui.



ster ?
Bash
IN
grep -n "func OpenWithStore" -A 30 /home/etouraille/src/vote/api/queel/rbac/store.go

OUT
67:func OpenWithStore(store queel.Store) *Store {
68-	return &Store{backend: newKVBackend(store)}
69-}
70-
71-// Close releases whatever Open opened. A no-op for a Store built with
72-// OpenWithStore — see its doc comment.
73-func (s *Store) Close() error {
74-	return s.backend.close()
75-}
76-
77-// CreateUser adds a new user with a freshly generated UUID.
78-func (s *Store) CreateUser(root bool, perms Permissions) (*User, error) {
79-	id, err := newUUID()
80-	if err != nil {
81-		return nil, err
82-	}
83-	return s.backend.createUser(id, root, perms)
84-}
85-
86-// CreateUserWithID adds a new user under a caller-chosen ID rather than a
87-// freshly generated one. It exists for bootstrapping: an operator launching
88-// queel for the first time needs one pre-existing root UUID (e.g. via
89-// QUEEL_ROOT_UUID) to hand to whatever will assign every other user's
90-// rights, before any other rbac user exists to do that assigning.
91-func (s *Store) CreateUserWithID(id string, root bool, perms Permissions) (*User, error) {
92-	return s.backend.createUser(id, root, perms)
93-}
94-
95-// GetUser fetches a single user by ID.
96-func (s *Store) GetUser(id string) (*User, error) {
97-	return s.backend.getUser(id)
Bash
IN
grep -rn "func newKVBackend" -A 15 /home/etouraille/src/vote/api/queel/rbac/*.go

OUT
/home/etouraille/src/vote/api/queel/rbac/store_kv.go:41:func newKVBackend(store queel.Store) *kvBackend {
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-42-	return &kvBackend{store: store}
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-43-}
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-44-
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-45-// close is a no-op: the caller owns store's lifecycle (it's shared with
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-46-// whatever domain repository is also using it), so closing it here would
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-47-// pull the rug out from under that.
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-48-func (b *kvBackend) close() error {
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-49-	return nil
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-50-}
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-51-
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-52-func (b *kvBackend) createUser(id string, root bool, perms Permissions) (*User, error) {
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-53-	if _, found, err := b.store.Get(userKey(id)); err != nil {
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-54-		return nil, err
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-55-	} else if found {
/home/etouraille/src/vote/api/queel/rbac/store_kv.go-56-		return nil, ErrAlreadyExists


donc en fait, si je comprends bien on pourrait lancer plusieurs instance de l'api derrière un load ballancer et laisser tourner les 3 cluster indépendamment pour la réplication des donnée, comme une base qui serait répliquée, l'api tappant indifférement sur l'un des trois cluster ?



Oui, votre compréhension est juste — et en creusant je trouve même une bonne nouvelle : le point qu'AMELIORATION.md présentait comme « le vrai bloquant » (rbac non répliqué) est en fait déjà corrigé dans le code actuel.

Ce qui rend ça possible : chaque nœud du cluster n'est pas un simple nœud de stockage — c'est le binaire vote-api complet (API publique + membre du cluster queel dans le même process). Et en mode cluster (main.go), toutes les données que l'API publique utilise passent par le même store répliqué :


queelStore = cluster.NewDistributedStore(coordinator)   // textes/rounds/fragments/votes/abonnements
...
rbacStore = rbac.OpenWithStore(queelStore)               // droits — MÊME store répliqué, pas un fichier local par nœud
rbac.OpenWithStore (vérifié dans queel/rbac/store_kv.go) délègue entièrement au queel.Store générique qu'on lui passe — il ne sait même pas si c'est un moteur local ou le DistributedStore répliqué en dessous. Donc côté queel (textes et droits), n'importe lequel des 3 nœuds peut légitimement répondre à n'importe quelle requête grâce au quorum de lecture/écriture du cluster.Coordinator — exactement comme vous le décrivez, une base répliquée derrière une couche API sans état.

Ce qui fait que ce n'est pas un caillou dans la chaussure :

Postgres (comptes/mots de passe/pseudo) et Qdrant/Ollama (recherche) sont des services partagés uniques, pas répliqués par queel — mais comme les 3 nœuds pointent vers la même instance (même .env), ils voient tous la même donnée, pas de risque d'incohérence entre nœuds sur ce point.
Le JWT est stateless et signé avec le même JWT_SECRET partagé — un token émis par n'importe quel nœud est vérifiable par les deux autres, pas besoin d'affinité de session.
Ce qui resterait à faire pour que ça tienne vraiment debout :

Le load balancer lui-même n'existe pas encore dans le dépôt (confirmé dans ma réponse précédente) — il faudrait en poser un devant les ports 8091/8092/8093.
Le port interne (9191-9193) doit rester strictement inter-nœuds, jamais exposé par ce load balancer — ces routes ne sont pas authentifiées par conception.

**Mise à jour** : fait — voir le service `lb` (nginx, round-robin) dans
`api/docker-compose.yml` et la section "Load balancing" du README de ce
répertoire. Reste un point local/dev (le port interne n'est de toute façon
pas exposé par ce nginx), pas encore un load-balancer multi-machines de
production.