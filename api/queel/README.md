make cluster-up                              # 3 nœuds par défaut, ports 9101-9103
make cluster-up CLUSTER_NODES=5 CLUSTER_BASE_PORT=9200   # personnalisable
make cluster-status                          # voir l'état des process
make cluster-down                            # arrêter les process (garde les données)
make cluster-clean                           # arrêter + supprimer les données


Tout passe par le même moteur LSM-tree (queel.Engine), mais chaque entité a son propre schéma de clé, défini dans repository.go. Les valeurs sont toutes encodées en JSON.

Schéma des clés
Entité	Clé	Valeur
Texte	text/<textID>	JSON Text{ID, Title, Content, Finalized, PreviousTextID, CreatedAt}
Tour de vote	round/<roundID>	JSON Round{ID, TextID, Number, Status, Slots, CreatedAt, ClosedAt}

Pointeur "tour en cours"	currentround/<textID>	juste l'ID du round (chaîne brute)
Compteur de rounds	roundcount/<textID>	le nombre de rounds jamais ouverts sur ce texte, en décimal (chaîne brute) — source du Round.Number suivant

Fragment	fragment/<fragmentID>	JSON Fragment{ID, TextID, SlotID, Content, AuthorID, CreatedAt}

Index fragment→slot	fragmentindex/<textID>/<slotID>/<fragmentID>	juste l'ID du fragment (pour le scan par préfixe)

Vote	vote/<fragmentID>/<userID>	JSON Vote{UserID, FragmentID, CreatedAt}
Choix courant d'un utilisateur	uservote/<textID>/<slotID>/<userID>	juste l'ID du fragment choisi
Point important : les slots n'ont pas de clé à eux. Un Slot{ID, Start, End, Round} n'existe qu'à l'intérieur du JSON d'un Round (dans son champ Slots []Slot) — puisqu'un slot n'a de sens que relativement au texte tel qu'il était au moment où ce tour de vote s'est ouvert.

Un Text est immuable une fois créé : fermer un round (CloseRound) ne modifie jamais le texte sur lequel il était ouvert. À la place, un nouveau Text est créé — nouvel ID, Finalized=true, PreviousTextID pointant vers l'ancien — avec le contenu des slots remplacé par les fragments gagnants. L'ancien texte, son round (désormais Status=closed), ses slots, fragments et votes restent tels quels, comme un historique permanent ; toute proposition ultérieure (ProposeEdit) doit viser le nouveau Text.ID. Le compteur de rounds (roundcount/<textID>) est initialisé sur le nouveau texte à partir du Number du round qui vient de se fermer, pour que la numérotation continue à travers la chaîne de versions plutôt que de repartir à 1 à chaque fork.

Sur disque (physiquement)
Dans QUEEL_DATA_DIR (défaut ./data) :

wal.log — journal d'écriture, les écritures récentes pas encore vidées sur disque
sstable-0.db, sstable-1.db, ... — fichiers triés immuables, produits quand la memtable se vide
Dans ces fichiers, chaque enregistrement est : longueur clé (4 octets) + clé + longueur valeur (4 octets, ou un marqueur spécial pour une suppression) + valeur — la valeur étant les octets JSON bruts décrits ci-dessus.

En mode cluster
Une couche supplémentaire s'ajoute : chaque valeur est enveloppée dans un cluster.Entry{Value, Timestamp, Tombstone} (lui aussi en JSON) avant d'être stockée localement sur chaque nœud — le JSON du domaine (Text/Round/Fragment/Vote) se retrouve donc niché dans Entry.Value. C'est cette enveloppe qui porte l'horodatage utilisé pour le last-write-wins entre réplicas.

Droits (rbac) et JWT
Indépendamment du moteur LSM-tree, le paquet rbac tient un annuaire droits/utilisateur dans un fichier plat JSON (voir rbac/model.go) : chaque utilisateur y est identifié par un UUID et porte des permissions (canVote, canCreateText, canCloseText, canSelect, canEditSelection, canUpdateText), plus un flag root qui court-circuite le tout. Cet annuaire est administré via un socket Unix local (rbac.ServeSocket) — pas d'authentification réseau, la permission du fichier du socket (0600) fait office de contrôle d'accès.

Table des routes HTTP de server.NewHandler face au droit rbac qu'elles exigent (quand QUEEL_JWT_SECRET est configuré) :
  POST /texts                                  canCreateText
  GET  /texts?limit=N                           (lecture, non gaté) — les N textes les plus récents (défaut 20, max 100)
  GET  /texts/{id}                              (lecture, non gaté)
  GET  /texts/{id}/with-slots                   (lecture, non gaté) — texte + slots de son round courant en un appel ; pas de round ouvert = roundNumber 0, slots []
  PUT  /texts/{id}                              canUpdateText
  POST /texts/{id}/propose-edit                 canSelect (nouvelle plage) ou canEditSelection (plage déjà ouverte dans le round)
  GET  /texts/{id}/round                         (lecture, non gaté)
  POST /texts/{id}/close-round                  canCloseText
  GET  /texts/{id}/slots/{slotId}/fragments      (lecture, non gaté)
  GET  /fragments/{id}                           (lecture, non gaté)
  POST /vote                                     canVote
  GET  /fragments/{id}/votes                     (lecture, non gaté)

queeld ouvre cet annuaire et sert ce socket dans les deux modes, cluster ou standalone :
  QUEEL_RBAC_PATH     chemin du fichier plat (défaut : QUEEL_DATA_DIR/rbac.json)
  QUEEL_RBAC_SOCKET   socket Unix d'administration (défaut : QUEEL_DATA_DIR/rbac.sock)
  QUEEL_ROOT_UUID     amorce un premier utilisateur root sous cet UUID précis au démarrage (idempotent — sans effet si déjà présent), pour avoir une identité root avant qu'aucune autre n'existe pour l'assigner
  QUEEL_JWT_SECRET    active la vérification des permissions sur les routes de mutation de server.NewHandler ; laissé vide, le serveur reste sans authentification comme avant

Les droits d'un utilisateur (rbac.User.Permissions + Root) sont encodés dans un PermBit puis signés dans un JWT HS256 (rbac.SignToken / rbac.VerifyToken, sans dépendance externe) par l'émetteur — typiquement l'api de ce repo au moment du login, voir api/auth.go. Un porteur du token peut alors vérifier ses propres droits (Claims.Allows) sans repasser par le socket rbac à chaque requête ; QUEEL_JWT_SECRET côté queeld doit correspondre au secret utilisé par l'émetteur.

The rbac package (flat-file UUID directory with canVote/canCreateText/canCloseText/canSelect/canEditSelection/canEditTextDirectly) already existed but was completely unwired. I connected it end to end:

queel/rbac/jwt.go — dependency-free HS256 JWT (Claims, SignToken, VerifyToken, Claims.Allows), consistent with this codebase's "stdlib crypto only" style. Tested in jwt_test.go.
queel/rbac/store.go — CreateUserWithID for bootstrapping a root user under a caller-chosen UUID.
Migration 003_add_rbac_uuid.sql — links an api User to its rbac UUID (nullable; set on first permission grant).
api/auth.go — login now looks up the user's rbac permissions and issues a signed JWT (replaces the old opaque, server-side session token — removed the now-dead Session/CreateSession machinery from api/store.go).
api/middleware.go — requireToken verifies the JWT and exposes rbac.Claims via context; added requirePermission helper.
api/admin.go (new) — PUT /api/admin/users/{id}/permissions, root-gated, transparently creates-or-updates the target user's rbac entry (this is where "the uuid is managed by the api" happens — the caller never sees or picks a UUID).
api/texts.go / api/slots.go — each mutating handler now checks the matching permission; proposeEdit distinguishes CanSelect (new range) vs CanEditSelection (joining an existing open slot).
cmd/queeld/main.go — in both standalone and cluster mode, opens the rbac store, optionally bootstraps a root user (QUEEL_ROOT_UUID), and serves the admin Unix socket (QUEEL_RBAC_SOCKET).
server/server.go — NewHandler now takes an optional JWT secret; when set, mutating routes enforce the same permission checks as the api layer. Passing nil keeps today's unauthenticated behavior (used by existing tests).
One thing worth flagging: the live api process embeds queel.Engine directly in-process rather than talking to queeld over HTTP, so the real enforcement path today is the api handlers, not server.go — I wired server.go too since it protects anyone using standalone/clustered queeld directly, but it isn't on api's current request path.

Both Go modules build, vet, and pass their tests (queel/rbac, queel/client, queel/cluster, queel itself). I did not touch the front end — the token stays an opaque bearer string from its point of view, so no changes were needed there. New/changed env vars are documented in api/.env.example and queel/README.md; I also added a JWT_SECRET to your local api/.env (server now fails fast without one) and noticed the checked-in api/go.mod was already missing the queel module requirement — go mod tidy fixed that as a side effect.



Cas normal : le process api (celui qu'utilise le front)
api/main.go embarque queel directement (pas de queeld séparé) et gère déjà le bootstrap root via QUEEL_ROOT_UUID. Étapes :


# 1. Génère un UUID pour le compte root
ROOT_UUID=$(uuidgen)   # ou : cat /proc/sys/kernel/random/uuid

# 2. Démarre l'api avec ce UUID — crée (une seule fois, idempotent)
#    un utilisateur rbac Root=true sous cet UUID exact
QUEEL_ROOT_UUID=$ROOT_UUID ./vote-api
Ça crée l'entrée dans le fichier plat rbac (QUEEL_DATA_DIR/rbac.json), mais pas encore un compte utilisable — il faut le relier à un vrai compte api (email/mot de passe) :


# 3. Crée un compte normal
curl -X POST localhost:8080/api/auth/register \
  -d '{"email":"admin@example.com","password":"........"}'
# ... valide l'email (code reçu par mail) ...

# 4. Récupère son ID (colonne `id` dans la table users), puis lie-le
#    manuellement au UUID root — c'est le seul pas manuel, inévitable
#    la première fois puisque PUT /api/admin/.../permissions exige
#    déjà un token Root pour l'appeler
psql "$DATABASE_URL" -c "UPDATE users SET rbac_uuid = '$ROOT_UUID' WHERE email = 'admin@example.com';"

# 5. Login normal — le JWT renvoyé porte maintenant Root: true
curl -X POST localhost:8080/api/auth/login -d '{"email":"admin@example.com","password":"........"}'
À partir de là, ce compte a tous les droits (Claims.Root court-circuite tous les checks), et peut assigner des droits aux autres via PUT /api/admin/users/{id}/permissions sans plus jamais toucher à la base à la main.

Si tu lances queeld séparément (standalone ou cluster)
Même variable, sur le process queeld cette fois :


QUEEL_ROOT_UUID=$ROOT_UUID QUEEL_DATA_DIR=./data ./queeld
# ou en cluster :
QUEEL_ROOT_UUID=$ROOT_UUID make cluster-up
Attention : c'est un annuaire rbac indépendant de celui de api sauf si tu pointes explicitement les deux sur le même fichier (QUEEL_RBAC_PATH identique des deux côtés) — ce qui n'est utile que si tu utilises vraiment queeld comme serveur à part (pas le cas aujourd'hui, api embarque tout).