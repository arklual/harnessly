-- One SSH key pair per space, for the «Свой сервер по SSH» way: a space's owners authorize the public half on their
-- servers, and only an install started by an owner of that space signs in with the private half. The private half is
-- encrypted (AES-256-GCM, bound to the space id) with a key the hub holds apart from the database.

CREATE TABLE space_ssh_keys (
    space_id        TEXT PRIMARY KEY REFERENCES spaces (id) ON DELETE CASCADE,
    public_key      TEXT NOT NULL,
    private_key_enc TEXT NOT NULL,
    created_at      BIGINT NOT NULL
);
