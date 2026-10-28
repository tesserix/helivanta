# MediConnect account implementation — #37

1. Add failing HTTP/PostgreSQL tests for account create/read/replay and isolation.
2. Implement account schema, subject RLS, audit and transactional creation event.
3. Add failing strict-input and versioned-preference tests, then implement them.
4. Register module and Global API owner; test the real proxy-to-owner path.
5. Generate contract inventory, run affected tests, race/build/vet/lint gates,
   document actual evidence and remaining #37/product dependencies.
