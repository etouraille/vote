// Points at the nginx reverse proxy in front of the 3-node queel cluster
// (api/docker-compose.yml's `lb` service, port 8090 — see
// api/queel/deploy/README.md's "Load balancing" section), round-robining
// across nodes 1-3 instead of talking to a single node. Requires both
// `make up` (from api/queel/deploy) and `docker compose up -d lb` (from
// api/) to be running.
export const API_BASE_URL = 'http://localhost:8090';
