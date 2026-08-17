package authz

// modelJSON is the complete Helivanta authorization model. It deliberately
// contains no role names and no permission names: roles and permissions
// are objects, and granting is a tuple write. This file changes only if
// the *shape* of authorization changes (for example when per-record or
// department scoping lands), never when a zone, permission or role is
// added.
//
//	role:<tenantID>/<roleKey>          assignee     user:<subject>
//	perm:<tenantID>/<permission>       granted_role role:<tenantID>/<roleKey>
//	tenant:<tenantID>                  granted_role role:<tenantID>/<roleKey>
//
// Tenant isolation lives in the object-id namespace, so a ListObjects
// for one tenant can never return another tenant's perm objects.
//
// `member` on tenant is DERIVED, never granted directly: it resolves
// through the same role assignment that grants permissions, so
// "member of this tenant" and "holds a role in this tenant" cannot
// drift apart. A directly-writable member relation would be a second,
// independent definition of membership and therefore a second thing to
// keep in sync.
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
    },
    {
      "type": "tenant",
      "relations": {
        "granted_role": { "this": {} },
        "member": {
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
