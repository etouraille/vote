
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