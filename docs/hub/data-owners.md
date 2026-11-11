# Who owns the data in the hub

Every table of the hub's database belongs to one kind of data, and every kind has an owner, a term, and an answer to
«what happens here when the owner deletes the original». The register is code: `DataKinds` in
[`internal/hub/store/owners.go`](../../internal/hub/store/owners.go). `TestEveryTableHasAnOwnerAndATerm` fails when a
table of the schema snapshots is in no kind, in two kinds, or when a kind names a table that is gone, so a migration
that adds a table also says whose data it holds and how long it lives.

An owner is one of three:

- **machine**: the machine's runtime has the original, the hub keeps a copy (the chats, the spend facts, the agent
  memory, what a machine reports about itself);
- **hub**: the hub's database is the only place the data is (accounts, spaces, roles, access, the outbox);
- **outside**: a provider or a code host has the original and the hub keeps what it read there.

«No term» in the register is a fact about the code, not a decision: nothing removes those rows by age. The first kind
with a term for the copy of something deleted at its owner is the chat history
([history.md](history.md#a-chat-deleted-on-its-machine)); the attachments of a deleted chat, the tombstones of the
agent memory and the reports of a machine that is gone are the next ones in line.

The register covers the tables of this database. The blobs of the spaces and the documents' store are kept elsewhere
and have terms of their own.
