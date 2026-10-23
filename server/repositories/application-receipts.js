// Operational receipts are never supplied by a backup and have no cascading FK.
export const APPLICATION_RECEIPTS_TABLE_SQL = `CREATE TABLE IF NOT EXISTS "application_creation_receipts" (
	"user_name" text NOT NULL,
	"idempotency_key" text NOT NULL,
	"generation" text NOT NULL,
	"request_hash" text NOT NULL,
	"created_application_id" integer,
	"created_at" text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	PRIMARY KEY("user_name", "idempotency_key"),
	CONSTRAINT "application_receipt_user" CHECK(length("application_creation_receipts"."user_name") BETWEEN 1 AND 160),
	CONSTRAINT "application_receipt_key" CHECK(length("application_creation_receipts"."idempotency_key") = 36),
	CONSTRAINT "application_receipt_generation" CHECK(length("application_creation_receipts"."generation") = 36),
	CONSTRAINT "application_receipt_hash" CHECK(length("application_creation_receipts"."request_hash") = 64 AND "application_creation_receipts"."request_hash" NOT GLOB '*[^0-9a-f]*'),
	CONSTRAINT "application_receipt_id" CHECK("application_creation_receipts"."created_application_id" IS NULL OR "application_creation_receipts"."created_application_id" > 0)
)`;
export async function ensureApplicationReceiptSchema(db) {
 await db.prepare(APPLICATION_RECEIPTS_TABLE_SQL).run();
}
// Caller must use a generation-bound DB when this read belongs to a live intent.
export async function readApplicationReceipt(db, userName, idempotencyKey) {
 return db.prepare(`SELECT user_name AS userName, idempotency_key AS idempotencyKey,
  generation, request_hash AS requestHash, created_application_id AS applicationId,
  created_at AS createdAt FROM application_creation_receipts
  WHERE user_name = ? AND idempotency_key = ?`).bind(userName, idempotencyKey).first();
}
