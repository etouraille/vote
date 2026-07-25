-- Links an api user to their entry in queel's rbac directory (see
-- queel/rbac). Null until an admin assigns permissions to the user via
-- PUT /api/admin/users/{id}/permissions, at which point it holds the UUID
-- of the corresponding rbac.User.
ALTER TABLE users ADD COLUMN IF NOT EXISTS rbac_uuid TEXT;
