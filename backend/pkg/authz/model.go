package authz

// modelJSON is the complete HMS authorization model. It deliberately
// contains no role names and no permission names: roles and permissions
// are objects, and granting is a tuple write. This file changes only if
// the *shape* of authorization changes (for example when per-record or
// department scoping lands), never when a zone, permission or role is
// added.
//
//	role:<tenantID>/<roleKey>          assignee     user:<subject>
//	perm:<tenantID>/<permission>       granted_role role:<tenantID>/<roleKey>
//
// Tenant isolation lives in the object-id namespace, so a ListObjects
// for one tenant can never return another tenant's perm objects.
const modelJSON = `{
  "schema_version": "1.1",
  "type_definitions": [
    { "type": "user" },
    {
      "type": "role",
      "relations": { "assignee": { "this": {} } },
      "metadata": {
        "relations": {
          "assignee": { "directly_related_user_types": [{ "type": "user" }] }
        }
      }
    },
    {
      "type": "perm",
      "relations": {
        "granted_role": { "this": {} },
        "can_do": {
          "tupleToUserset": {
            "tupleset": { "relation": "granted_role" },
            "computedUserset": { "relation": "assignee" }
          }
        }
      },
      "metadata": {
        "relations": {
          "granted_role": { "directly_related_user_types": [{ "type": "role" }] }
        }
      }
    }
  ]
}`
