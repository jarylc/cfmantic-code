# User-like search smoke (@explorer subagent)

**Session ID:** ses_2fadbd54cffeSSUhEOXsLQtLW5
**Created:** 3/19/2026, 4:09:09 PM
**Updated:** 3/19/2026, 4:09:22 PM

---

## User

Task: Run a small set of user-like semantic searches against the already-indexed repo `/home/jaryl/Projects/cf-workers-milvus` to smoke-test retrieval quality after deployment.

Acceptance criteria:
- Use the existing `cfmantic-code` index/search workflow for this repo; do not reindex unless strictly necessary.
- Run these natural-language searches and evaluate whether the returned hits are relevant:
  1. "where are Vectorize metadata filters compiled"
  2. "how does collection drop restore deleted vectors"
  3. "where does Workers AI rerank get the query text"
- For each search, return:
  - the query
  - whether the results looked relevant (`yes` / `mixed` / `no`)
  - top 3-5 hits with file paths and brief reasons
  - any obvious misses or caveats
- Keep the output concise and reference files/lines; do not paste large code blocks.

Relevant paths/files:
- Repo root: `/home/jaryl/Projects/cf-workers-milvus`
- Likely relevant files include `src/search.ts`, `src/filters.ts`, `src/collections.ts`, `src/entities.ts`, `src/rerank.ts`, and related tests.

Constraints and non-goals:
- Read-only only.
- No file edits.
- No reindex unless the existing index is unavailable/stale enough to block the search.
- Focus on retrieval quality, not implementation critique.

Ownership boundary / handoff:
- Own only the semantic-search smoke test and retrieval-quality summary. I will summarize the outcome to the user.

MCP Status: cfmantic-code=indexed

---

## Assistant

**Tool: cfmantic-code_get_indexing_status**

**Input:**
```json
{
  "path": "/home/jaryl/Projects/cf-workers-milvus"
}
```

**Output:**
```
Index complete
Files: 39
Chunks: 150
Last updated: 2026-03-19T16:07:20+08:00
Path: /home/jaryl/Projects/cf-workers-milvus
Status: indexed
Operation: indexing
Source: manual
Mode: full
Started: 2026-03-19T16:06:39+08:00
Step updated: 2026-03-19T16:06:39+08:00
Last progress: 2026-03-19T16:07:20+08:00
```

**Tool: cfmantic-code_search_code**

**Input:**
```json
{
  "path": "/home/jaryl/Projects/cf-workers-milvus",
  "query": "where are Vectorize metadata filters compiled",
  "limit": 8
}
```

**Output:**
```
Found 8 results for "where are Vectorize metadata filters compiled":

### 1. worker-configuration.d.ts (lines 12381-12513)
Symbol: VectorizeIndexDetails (interface, lines 12440-12451)
```ts
/**
 * Filter criteria for vector metadata used to limit the retrieved query result set.
 */
type VectorizeVectorMetadataFilter = {
	[field: string]:
		| Exclude<VectorizeVectorMetadataValue, string[]>
		| null
		| {
				[Op in VectorizeVectorMetadataFilterOp]?: Exclude<
					VectorizeVectorMetadataValue,
					string[]
				> | null;
		  }
		| {
				[Op in VectorizeVectorMetadataFilterCollectionOp]?: Exclude<
					VectorizeVectorMetadataValue,
					string[]
				>[];
		  };
};
/**
 * Supported distance metrics for an index.
 * Distance metrics determine how other "similar" vectors are determined.
 */
type VectorizeDistanceMetric = "euclidean" | "cosine" | "dot-product";
/**
 * Metadata return levels for a Vectorize query.
 *
 * Default to "none".
 *
 * @property all      Full metadata for the vector return set, including all fields (including those un-indexed) without truncation. This is a more expensive retrieval, as it requires additional fetching & reading of un-indexed data.
 * @property indexed  Return all metadata fields configured for indexing in the vector return set. This level of retrieval is "free" in that no additional overhead is incurred returning this data. However, note that indexed metadata is subject to truncation (especially for larger strings).
 * @property none     No indexed metadata will be returned.
 */
type VectorizeMetadataRetrievalLevel = "all" | "indexed" | "none";
interface VectorizeQueryOptions {
	topK?: number;
	namespace?: string;
	returnValues?: boolean;
	returnMetadata?: boolean | VectorizeMetadataRetrievalLevel;
	filter?: VectorizeVectorMetadataFilter;
}
/**
 * Information about the configuration of an index.
 */
type VectorizeIndexConfig =
	| {
			dimensions: number;
			metric: VectorizeDistanceMetric;
	  }
	| {
			preset: string; // keep this generic, as we'll be adding more presets in the future and this is only in a read capacity
	  };
/**
 * Metadata about an existing index.
 *
 * This type is exclusively for the Vectorize **beta** and will be deprecated once Vectorize RC is released.
 * See {@link VectorizeIndexInfo} for its post-beta equivalent.
 */
interface VectorizeIndexDetails {
	/** The unique ID of the index */
	readonly id: string;
	/** The name of the index. */
	name: string;
	/** (optional) A human readable description for the index. */
	description?: string;
	/** The index configuration, including the dimension size and distance metric. */
	config: VectorizeIndexConfig;
	/** The number of records containing vectors within the index. */
	vectorsCount: number;
}
/**
 * Metadata about an existing index.
 */
interface VectorizeIndexInfo {
	/** The number of records containing vectors within the index. */
	vectorCount: number;
	/** Number of dimensions the index has been configured for. */
	dimensions: number;
	/** ISO 8601 datetime of the last processed mutation on in the index. All changes before this mutation will be reflected in the index state. */
	processedUpToDatetime: number;
	/** UUIDv4 of the last mutation processed by the index. All changes before this mutation will be reflected in the index state. */
	processedUpToMutation: number;
}
/**
 * Represents a single vector value set along with its associated metadata.
 */
interface VectorizeVector {
	/** The ID for the vector. This can be user-defined, and must be unique. It should uniquely identify the object, and is best set based on the ID of what the vector represents. */
	id: string;
	/** The vector values */
	values: VectorFloatArray | number[];
	/** The namespace this vector belongs to. */
	namespace?: string;
	/** Metadata associated with the vector. Includes the values of other fields and potentially additional details. */
	metadata?: Record<string, VectorizeVectorMetadata>;
}
/**
 * Represents a matched vector for a query along with its score and (if specified) the matching vector information.
 */
type VectorizeMatch = Pick<Partial<VectorizeVector>, "values"> &
	Omit<VectorizeVector, "values"> & {
		/** The score or rank for similarity, when returned as a result */
		score: number;
	};
/**
 * A set of matching {@link VectorizeMatch} for a particular query.
 */
interface VectorizeMatches {
	matches: VectorizeMatch[];
	count: number;
}
/**
 * Results of an operation that performed a mutation on a set of vectors.
 * Here, `ids` is a list of vectors that were successfully processed.
 *
 * This type is exclusively for the Vectorize **beta** and will be deprecated once Vectorize RC is released.
 * See {@link VectorizeAsyncMutation} for its post-beta equivalent.
 */
interface VectorizeVectorMutation {
	/* List of ids of vectors that were successfully processed. */
	ids: string[];
	/* Total count of the number of processed vectors. */
	count: number;
}
/**
 * Result type indicating a mutation on the Vectorize Index.
 * Actual mutations are processed async where the `mutationId` is the unique identifier for the operation.
 */
interface VectorizeAsyncMutation {
	/** The unique identifier for the async mutation operation containing the changeset. */
	mutationId: string;
}

```

### 2. src/search.ts (lines 1-301)
```ts
import {
	Env,
	EntityData,
	EntityRow,
	MilvusSearchRequest,
	MilvusHybridSearchRequest,
	getConfiguredVectorizeDimension,
	ok,
	err,
	validateDefaultDbName,
	toSafeName,
	toPhysicalVectorizeIds,
	ENTITY_FIELDS,
	VectorizeEntityMetadata,
} from "./types";
import { rrfFuse, RankedResult } from "./rrf";
import { InvalidEmbeddingResponseError, embedQuery } from "./embedding";
import { filterFields } from "./entities";
import {
	compileFilterPlanToSql,
	compileFilterPlanToVectorize,
	isUnsupportedFilterError,
	parseFilterPlan,
} from "./filters";
import { rerankWithWorkersAI, resolveHybridRerankQuery } from "./rerank";

export const VECTORIZE_METADATA_TOPK_CAP = 20;
export const VECTORIZE_IDS_ONLY_TOPK_CAP = 100;

const DEFAULT_VECTORIZE_TOPK = 10;
const HYBRID_RERANK_POOL_MULTIPLIER = 5;
const HYBRID_RERANK_POOL_MIN = 20;
const HYBRID_RERANK_POOL_MAX = 100;
const SPARSE_RERANK_POOL_MULTIPLIER = 3;
const SPARSE_RERANK_POOL_MIN = 20;
const SPARSE_RERANK_POOL_MAX = 50;
const D1_MAX_BOUND_PARAMS = 100;

type CollectionVectorMapping = {
	id: string;
	physical_id: string;
};

function parseOptionalFilterPlan(
	filter: string | undefined,
):
	| { ok: true; plan: ReturnType<typeof parseFilterPlan> | null }
	| { ok: false; response: Response } {
	if (!filter?.trim()) return { ok: true, plan: null };

	try {
		return { ok: true, plan: parseFilterPlan(filter) };
	} catch (error) {
		if (isUnsupportedFilterError(error)) {
			return { ok: false, response: err(1, error.message) };
		}
		throw error;
	}
}

function extractEntityFields(
	meta: VectorizeEntityMetadata | undefined,
): EntityData {
	return {
		content: meta?.content ?? "",
		relativePath: meta?.relativePath ?? "",
		startLine: meta?.startLine ?? 0,
		endLine: meta?.endLine ?? 0,
		fileExtension: meta?.fileExtension ?? "",
		metadata: meta?.metadata ?? "{}",
	};
}

function validateVectorDimension(env: Env, vector: number[]): Response | null {
	const configuredDimension = getConfiguredVectorizeDimension(env);
	if (vector.length === configuredDimension) return null;
	return err(
		1,
		`Vector dimension ${vector.length} does not match configured Vectorize dimension ${configuredDimension}`,
	);
}

function mapEmbeddingResponseError(error: unknown): Response | null {
	if (error instanceof InvalidEmbeddingResponseError) {
		return err(1, error.message);
	}

	return null;
}

function validateOptionalPositiveInteger(
	value: unknown,
	label: string,
): Response | null {
	if (value === undefined) return null;
	if (
		typeof value === "number" &&
		Number.isFinite(value) &&
		Number.isInteger(value) &&
		value > 0
	) {
		return null;
	}

	return err(1, `Invalid ${label}: must be a finite positive integer`);
}

function clampVectorizeTopK(limit: number | undefined): number {
	return Math.min(limit ?? DEFAULT_VECTORIZE_TOPK, VECTORIZE_IDS_ONLY_TOPK_CAP);
}

function resolveLimit(limit: number | undefined): number {
	return limit ?? DEFAULT_VECTORIZE_TOPK;
}

function resolveHybridRerankPoolLimit(finalLimit: number): number {
	return Math.min(
		Math.max(
			finalLimit * HYBRID_RERANK_POOL_MULTIPLIER,
			HYBRID_RERANK_POOL_MIN,
		),
		HYBRID_RERANK_POOL_MAX,
	);
}

function resolveSparseRerankPoolLimit(finalLimit: number): number {
	return Math.min(
		Math.max(
			finalLimit * SPARSE_RERANK_POOL_MULTIPLIER,
			SPARSE_RERANK_POOL_MIN,
		),
		SPARSE_RERANK_POOL_MAX,
	);
}

function getVectorizeReturnMetadata(
	topK: number,
): VectorizeMetadataRetrievalLevel {
	return topK <= VECTORIZE_METADATA_TOPK_CAP ? "all" : "none";
}

function extractEntityFieldsFromRecord(
	data: Record<string, unknown> | undefined,
): EntityData {
	return extractEntityFields(data as VectorizeEntityMetadata | undefined);
}

function hasEntityFields(data: Record<string, unknown>): boolean {
	return ENTITY_FIELDS.every((field) => Object.hasOwn(data, field));
}

async function hydrateEntityFieldsById(
	env: Env,
	collectionName: string,
	safeName: string,
	ids: string[],
): Promise<Map<string, EntityData>> {
	const uniqueIds = Array.from(new Set(ids));
	const hydrated = new Map<string, EntityData>();

	for (
		let offset = 0;
		offset < uniqueIds.length;
		offset += VECTORIZE_METADATA_TOPK_CAP
	) {
		const logicalIds = uniqueIds.slice(
			offset,
			offset + VECTORIZE_METADATA_TOPK_CAP,
		);
		const physicalIds = await toPhysicalVectorizeIds(
			collectionName,
			logicalIds,
		);
		const logicalIdByPhysicalId = new Map(
			physicalIds.map((physicalId, index) => [physicalId, logicalIds[index]]),
		);
		const vectors = await env.VECTORIZE.getByIds(physicalIds);

		for (const vector of vectors) {
			if (vector.namespace && vector.namespace !== safeName) continue;
			const logicalId = logicalIdByPhysicalId.get(vector.id);
			if (!logicalId) continue;
			hydrated.set(
				logicalId,
				extractEntityFields(
					vector.metadata as VectorizeEntityMetadata | undefined,
				),
			);
		}
	}

	return hydrated;
}

async function maskDenseMatchesByLiveIds(
	env: Env,
	collectionName: string,
	matches: VectorizeMatch[],
): Promise<VectorizeMatch[]> {
	if (matches.length === 0) return matches;

	const logicalIdByPhysicalId = new Map<string, string>();
	const uniqueIds = Array.from(new Set(matches.map((match) => match.id)));
	const maxIdsPerStmt = D1_MAX_BOUND_PARAMS - 1;

	for (let offset = 0; offset < uniqueIds.length; offset += maxIdsPerStmt) {
		const ids = uniqueIds.slice(offset, offset + maxIdsPerStmt);
		const placeholders = ids.map(() => "?").join(", ");
		const { results } = await env.DB.prepare(
			`SELECT id, physical_id FROM collection_vectors WHERE collection_name = ? AND physical_id IN (${placeholders})`,
		)
			.bind(collectionName, ...ids)
			.run<CollectionVectorMapping>();

		for (const row of results) {
			logicalIdByPhysicalId.set(row.physical_id, row.id);
		}
	}

	return matches.flatMap((match) => {
		const logicalId = logicalIdByPhysicalId.get(match.id);
		return logicalId ? [{ ...match, id: logicalId }] : [];
	});
}

async function queryDenseRankedResults(
	env: Env,
	queryVector: number[],
	collectionName: string,
	safeName: string,
	topK: number,
	forceIdsOnlyMode: boolean,
	vectorizeFilter: VectorizeVectorMetadataFilter | undefined,
): Promise<{ results: RankedResult[]; usesIdsOnlyMode: boolean }> {
	const returnMetadata = forceIdsOnlyMode
		? "none"
		: getVectorizeReturnMetadata(topK);
	const matches = await env.VECTORIZE.query(queryVector, {
		topK,
		namespace: safeName,
		returnMetadata,
		returnValues: false,
		...(vectorizeFilter && { filter: vectorizeFilter }),
	});
	const liveMatches = await maskDenseMatchesByLiveIds(
		env,
		collectionName,
		matches.matches || [],
	);

	return {
		usesIdsOnlyMode: returnMetadata === "none",
		results: liveMatches.map((match) => ({
			id: match.id,
			score: match.score,
			data:
				returnMetadata === "all"
					? extractEntityFields(
							match.metadata as VectorizeEntityMetadata | undefined,
						)
					: {},
		})),
	};
}

async function hydrateRankedResults<T extends RankedResult>(
	env: Env,
	collectionName: string,
	safeName: string,
	results: T[],
): Promise<T[]> {
	if (results.length === 0) return results;

	const idsToHydrate = Array.from(
		new Set(
			results
				.filter((result) => !hasEntityFields(result.data))
				.map((result) => result.id),
		),
	);

	if (idsToHydrate.length === 0) return results;

	const hydratedById = await hydrateEntityFieldsById(
		env,
		collectionName,
		safeName,
		idsToHydrate,
	);

	return results.map((result) => {
		if (hasEntityFields(result.data)) return result;

		return {
			...result,
			data:
				hydratedById.get(result.id) ??
				extractEntityFieldsFromRecord(result.data),
		} as T;
	});
}


```

### 3. test/collections.spec.ts (lines 592-874)
```ts
							relativePath: `${id}.ts`,
							startLine: 1,
							endLine: 2,
							fileExtension: ".ts",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			vectors.filter((vector) => ids.includes(vector.id)),
		);
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		const upsertSpy = vi.spyOn(e.VECTORIZE, "upsert").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.DB, "batch").mockRejectedValueOnce(
			new Error("D1 cleanup failed"),
		);

		await expect(
			dropCollection(e, { collectionName: "rollback_drop" }),
		).rejects.toThrow("D1 cleanup failed");
		expect(e.VECTORIZE.deleteByIds).toHaveBeenCalledWith([
			await physicalId("rollback_drop", "v1"),
			await physicalId("rollback_drop", "v2"),
		]);
		expect(upsertSpy).toHaveBeenCalledWith(vectors);
	});

	it("skips mismatched namespaces and logs failed rollback restores", async () => {
		await createCollection(e, {
			collectionName: "rollback_mismatch",
			schema: validSchema(128, true),
		});
		await trackVectorId("rollback_mismatch", "v1");
		await trackVectorId("rollback_mismatch", "v2");

		const vectors = await Promise.all(
			["v1", "v2"].map(
				async (id) =>
					({
						id: await physicalId("rollback_mismatch", id),
						values: [0.1, 0.2, 0.3],
						namespace: id === "v1" ? "other_namespace" : "rollback_mismatch",
						metadata: {
							content: id,
							relativePath: `${id}.ts`,
							startLine: 1,
							endLine: 2,
							fileExtension: ".ts",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			vectors.filter((vector) => ids.includes(vector.id)),
		);
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.VECTORIZE, "upsert").mockRejectedValueOnce(
			new Error("rollback restore failed"),
		);
		const consoleErrorSpy = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});
		vi.spyOn(e.DB, "batch").mockRejectedValueOnce(
			new Error("D1 cleanup failed"),
		);

		await expect(
			dropCollection(e, { collectionName: "rollback_mismatch" }),
		).rejects.toThrow("D1 cleanup failed");
		expect(e.VECTORIZE.deleteByIds).toHaveBeenCalledWith([
			await physicalId("rollback_mismatch", "v1"),
			await physicalId("rollback_mismatch", "v2"),
		]);
		expect(consoleErrorSpy).toHaveBeenCalled();
	});

	it("does not attempt rollback when drop cleanup fails before any vectors are recovered", async () => {
		await createCollection(e, {
			collectionName: "rollback_empty",
			schema: validSchema(128, true),
		});
		await trackVectorId("rollback_empty", "v1");

		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue([]);
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		const upsertSpy = vi.spyOn(e.VECTORIZE, "upsert").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		const consoleErrorSpy = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});
		vi.spyOn(e.DB, "batch").mockRejectedValueOnce(
			new Error("D1 cleanup failed"),
		);

		await expect(
			dropCollection(e, { collectionName: "rollback_empty" }),
		).rejects.toThrow("D1 cleanup failed");
		expect(upsertSpy).not.toHaveBeenCalled();
		expect(consoleErrorSpy).not.toHaveBeenCalled();
	});

	it("limits concurrent Vectorize deletes while dropping a collection", async () => {
		await seedCollection("bounded_drop");
		const ids = Array.from({ length: 550 }, (_, i) => `v${i}`);
		for (const id of ids) {
			await trackVectorId("bounded_drop", id);
		}

		const resolvers: Array<() => void> = [];
		const deleteSpy = vi.spyOn(e.VECTORIZE, "deleteByIds").mockImplementation(
			() =>
				new Promise((resolve) => {
					resolvers.push(() =>
						resolve({
							ids: [],
							count: 0,
							mutationId: "mock",
						} as VectorMutation),
					);
				}),
		);
		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			ids.map(
				(id) =>
					({
						id,
						values: [0.1, 0.2, 0.3],
						namespace: "bounded_drop",
						metadata: {
							content: "",
							relativePath: "",
							startLine: 0,
							endLine: 0,
							fileExtension: "",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		const dropPromise = dropCollection(e, { collectionName: "bounded_drop" });

		const initialConcurrency = 5;
		await vi.waitFor(() =>
			expect(deleteSpy).toHaveBeenCalledTimes(initialConcurrency),
		);

		for (const resolve of resolvers.splice(0, initialConcurrency)) resolve();

		await vi.waitFor(() => expect(deleteSpy).toHaveBeenCalledTimes(6));
		resolvers.splice(0).forEach((resolve) => resolve());

		const json = await (await dropPromise).json<{ code: number }>();
		expect(json.code).toBe(0);
	});

	it("pages tracked vector IDs while dropping a collection", async () => {
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			ids.map(
				(id) =>
					({
						id,
						values: [0.1, 0.2, 0.3],
						namespace: "paged_drop",
						metadata: {
							content: "",
							relativePath: "",
							startLine: 0,
							endLine: 0,
							fileExtension: "",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		await seedCollection("paged_drop");
		const ids = Array.from({ length: 501 }, (_, i) => `v${i}`);
		for (const id of ids) {
			await trackVectorId("paged_drop", id);
		}

		const prepareSpy = vi.spyOn(e.DB, "prepare");

		const res = await dropCollection(e, { collectionName: "paged_drop" });
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(0);

		const trackedIdSelects = prepareSpy.mock.calls
			.map(([sql]) => sql)
			.filter((sql) =>
				sql.startsWith(
					"SELECT physical_id FROM collection_vectors WHERE collection_name = ?",
				),
			);

		expect(trackedIdSelects).toHaveLength(2);
		expect(trackedIdSelects.every((sql) => sql.includes("LIMIT"))).toBe(true);
	});

	it("deletes Vectorize vectors for non-hybrid collections using collection_vectors tracking", async () => {
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			ids.map(
				(id) =>
					({
						id,
						values: [0.1, 0.2, 0.3],
						namespace: "non_hybrid_col",
						metadata: {
							content: "",
							relativePath: "",
							startLine: 0,
							endLine: 0,
							fileExtension: "",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		await seedCollection("non_hybrid_col", 128, 0);
		await trackVectorId("non_hybrid_col", "v1");
		await trackVectorId("non_hybrid_col", "v2");

		const res = await dropCollection(e, { collectionName: "non_hybrid_col" });
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(0);
		expect(e.VECTORIZE.deleteByIds).toHaveBeenCalledWith([
			await physicalId("non_hybrid_col", "v1"),
			await physicalId("non_hybrid_col", "v2"),
		]);

		// collection_vectors should be cleaned up
		const { results } = await e.DB.prepare(
			"SELECT * FROM collection_vectors WHERE collection_name = ?",
		)
			.bind("non_hybrid_col")
			.run();
		expect(results).toHaveLength(0);

		// collection row should be deleted
		const row = await e.DB.prepare("SELECT * FROM collections WHERE name = ?")
			.bind("non_hybrid_col")
			.first();
		expect(row).toBeNull();
	});

	it("returns ok gracefully when the collection does not exist", async () => {

```

### 4. test/search.spec.ts (lines 1-199)
Symbol: trackLiveIds (function, lines 93-114)
```ts
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { env } from "cloudflare:test";
import type {
	Env,
	MilvusHybridSearchRequest,
	MilvusSearchRequest,
} from "../src/types";
import { toPhysicalVectorizeId } from "../src/types";
import {
	searchEntities,
	hybridSearch,
	VECTORIZE_METADATA_TOPK_CAP,
	VECTORIZE_IDS_ONLY_TOPK_CAP,
} from "../src/search";
import * as embedding from "../src/embedding";
import * as filters from "../src/filters";

// The test env from cloudflare:test is typed as ProvidedEnv but the LSP picks up
// the root tsconfig which excludes test/ — cast once at module level.
const e = env as unknown as Env;

beforeEach(() => {
	e.VECTORIZE_DIMENSION = "3";
});

type Envelope<T extends Record<string, unknown> = Record<string, unknown>> = {
	code: number;
	data: T[];
	message?: string;
};

type SearchResult = {
	id: string;
	distance: number;
	score?: number;
	rrfScore?: number;
	content?: string;
	relativePath?: string;
	startLine?: number;
	endLine?: number;
	fileExtension?: string;
	metadata?: string;
};

function vectorizeMatches(
	matches: VectorizeMatch[] = [],
	count = matches.length,
): VectorizeMatches {
	return { count, matches };
}

function partialVectorizeMatches(
	matches: Partial<VectorizeMatches>,
): VectorizeMatches {
	return matches as unknown as VectorizeMatches;
}

function vectorizeMatch(match: {
	id: string;
	score: number;
	metadata?: Record<string, unknown> | null;
}): VectorizeMatch {
	return match as unknown as VectorizeMatch;
}

function vectorizeVector(vector: {
	id: string;
	namespace?: string;
	metadata?: Record<string, unknown> | null;
}): VectorizeVector {
	return { values: [], ...vector } as unknown as VectorizeVector;
}

function makeEmbedResponse(vecs: number[][]): AiTextEmbeddingsOutput {
	return { data: vecs, shape: [vecs.length, vecs[0]?.length ?? 0] };
}

type RerankRunOutput = Awaited<ReturnType<Env["AI"]["run"]>>;

function makeRerankResponse(
	response: Array<{ id: number; score: number }>,
): RerankRunOutput {
	return { response } as unknown as RerankRunOutput;
}

async function physicalId(
	collectionName: string,
	logicalId: string,
): Promise<string> {
	return toPhysicalVectorizeId(collectionName, logicalId);
}

async function trackLiveIds(
	collectionName: string,
	ids: string[],
): Promise<void> {
	for (let offset = 0; offset < ids.length; offset += 33) {
		const chunk = ids.slice(offset, offset + 33);
		const physicalIds = await Promise.all(
			chunk.map((id) => physicalId(collectionName, id)),
		);
		const placeholders = chunk.map(() => "(?, ?, ?)").join(", ");
		const params = chunk.flatMap((id, index) => [
			id,
			physicalIds[index],
			collectionName,
		]);
		await e.DB.prepare(
			`INSERT OR REPLACE INTO collection_vectors (id, physical_id, collection_name) VALUES ${placeholders}`,
		)
			.bind(...params)
			.run();
	}
}

async function seedDefaultTrackedIds(): Promise<void> {
	const ids = [
		...Array.from({ length: 36 }, (_, i) => `id${i}`),
		...Array.from({ length: 25 }, (_, i) => `dense${i}`),
		"id1",
		"id2",
		"id3",
		"null_meta",
		"shared1",
		"sparse_meta",
	];

	await trackLiveIds("test_col", ids);
	await trackLiveIds("hybrid_col", ids);
}

async function createFtsTable(collectionName: string): Promise<void> {
	const safeName = collectionName.replaceAll(/\W/g, "_");
	await e.DB.exec(`DROP TABLE IF EXISTS fts_${safeName}`);
	await e.DB.exec(
		`CREATE VIRTUAL TABLE IF NOT EXISTS fts_${safeName} USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
	);
}

const MOCK_MATCHES: VectorizeMatch[] = [
	{
		id: "id1",
		score: 0.95,
		metadata: {
			content: "hello",
			relativePath: "a.ts",
			startLine: 1,
			endLine: 5,
			fileExtension: ".ts",
			metadata: "{}",
		},
	},
	{
		id: "id2",
		score: 0.8,
		metadata: {
			content: "world",
			relativePath: "b.ts",
			startLine: 1,
			endLine: 3,
			fileExtension: ".ts",
			metadata: "{}",
		},
	},
];

async function vectorizeMatchesFor(
	collectionName: string,
	matches: VectorizeMatch[] = MOCK_MATCHES,
): Promise<VectorizeMatches> {
	return vectorizeMatches(
		await Promise.all(
			matches.map(async (match) =>
				vectorizeMatch({
					...match,
					id: await physicalId(collectionName, match.id),
				}),
			),
		),
	);
}

async function vectorizeVectorsFor(
	collectionName: string,
	vectors: Array<{
		id: string;
		namespace?: string;
		metadata?: Record<string, unknown> | null;
	}>,
): Promise<VectorizeVector[]> {
	return Promise.all(
		vectors.map(async (vector) =>
			vectorizeVector({
				...vector,
				id: await physicalId(collectionName, vector.id),
			}),
		),
	);
}


```

### 5. test/entities.spec.ts (lines 1592-1816)
```ts
				`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
			);
			await e.DB.exec(`DELETE FROM fts_hybrid_col`);

			for (const [id, ext] of [
				["e1", "ts"],
				["e2", "go"],
				["e3", "py"],
			]) {
				await e.DB.prepare(
					`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				)
					.bind(id, `${ext} content`, `src/file.${ext}`, 1, 10, ext, "{}")
					.run();
				await e.DB.prepare(
					`INSERT INTO collection_vectors (id, physical_id, collection_name) VALUES (?, ?, ?)`,
				)
					.bind(id, await physicalId("hybrid_col", id), "hybrid_col")
					.run();
			}

			vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
				ids: ["e1", "e2"],
				count: 2,
			} as unknown as VectorizeVectorMutation);

			const body = {
				collectionName: "hybrid_col",
				filter: 'fileExtension in ["ts", "go"]',
			};
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);
			expect(e.VECTORIZE.deleteByIds).toHaveBeenCalled();

			const { results: ftsRows } = await e.DB.prepare(
				"SELECT id FROM fts_hybrid_col",
			).run<{ id: string }>();
			expect(ftsRows.map((r) => r.id)).toEqual(["e3"]);

			const { results: cvRows } = await e.DB.prepare(
				"SELECT id FROM collection_vectors WHERE collection_name = 'hybrid_col'",
			).run<{ id: string }>();
			expect(cvRows.map((r) => r.id)).toEqual(["e3"]);
		});

		it("deletes entities matching an ancestor-path prefix filter", async () => {
			await createHybridCollection("hybrid_col");
			await seedTrackedHybridEntities("hybrid_col", ["e1"], {
				relativePath: "src/keep/a.ts",
				fileExtension: ".ts",
			});
			await seedTrackedHybridEntities("hybrid_col", ["e2"], {
				relativePath: "src/keep/b.ts",
				fileExtension: ".ts",
			});
			await seedTrackedHybridEntities("hybrid_col", ["e3"], {
				relativePath: "src/other/c.ts",
				fileExtension: ".ts",
			});

			const body = {
				collectionName: "hybrid_col",
				filter: 'relativePath like "src/keep/%"',
			};
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);

			const { results } = await e.DB.prepare(
				"SELECT id FROM fts_hybrid_col ORDER BY id",
			).run<{ id: string }>();
			expect(results.map((row) => row.id)).toEqual(["e3"]);
		});

		it("treats internal % and _ literally in delete prefix filters", async () => {
			await createHybridCollection("hybrid_col");
			await seedTrackedHybridEntities("hybrid_col", ["literal"], {
				relativePath: "src/_tmp%/keep.ts",
				fileExtension: ".ts",
			});
			await seedTrackedHybridEntities("hybrid_col", ["keep"], {
				relativePath: "src/_tmpx/keep.ts",
				fileExtension: ".ts",
			});

			const res = await deleteEntities(e, {
				collectionName: "hybrid_col",
				filter: 'relativePath like "src/_tmp%%"',
			});
			const json = await readEnvelope<MutationData>(res);

			expect(json.code).toBe(0);
			expect(e.VECTORIZE.deleteByIds).toHaveBeenCalledWith([
				await physicalId("hybrid_col", "literal"),
			]);

			const { results } = await e.DB.prepare(
				"SELECT id FROM fts_hybrid_col ORDER BY id",
			).run<{ id: string }>();
			expect(results.map((row) => row.id)).toEqual(["keep"]);
		});

		it("rejects compound filters that start with an id-list clause", async () => {
			await createHybridCollection("hybrid_col");
			const res = await deleteEntities(e, {
				collectionName: "hybrid_col",
				filter: 'id in ["e1"] and fileExtension == ".ts"',
			});
			const json = await readEnvelope<MutationData>(res);

			expect(json.code).toBe(1);
			expect(json.message).toMatch(/unsupported filter/i);
			expect(e.VECTORIZE.deleteByIds).not.toHaveBeenCalled();
		});
	});

	describe("queryEntities", () => {
		beforeEach(async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('query_col', 3, 1)`,
			);
			await e.DB.exec(
				`CREATE VIRTUAL TABLE IF NOT EXISTS fts_query_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
			);
			await e.DB.exec(`DELETE FROM fts_query_col`);

			const rows: [string, string, string, number, number, string, string][] = [
				["e1", "alpha content", "src/alpha.ts", 1, 10, ".ts", "{}"],
				["e2", "beta content", "src/beta.ts", 1, 5, ".ts", "{}"],
				["e3", "gamma content", "src/alpha.ts", 11, 20, ".ts", "{}"],
			];
			for (const row of rows) {
				await e.DB.prepare(
					`INSERT INTO fts_query_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				)
					.bind(...row)
					.run();
			}
		});

		it("returns only rows matching the relativePath filter", async () => {
			const body = {
				collectionName: "query_col",
				filter: 'relativePath == "src/alpha.ts"',
			};
			const res = await queryEntities(e, body);
			const json = await readEnvelope<QueryRow[]>(res);
			expect(json.code).toBe(0);
			expect(json.data).toHaveLength(2);
			const ids = json.data.map((r) => r.id);
			expect(ids).toContain("e1");
			expect(ids).toContain("e3");
		});

		it("returns all rows when filter has no relativePath clause", async () => {
			const body = { collectionName: "query_col", filter: "" };
			const res = await queryEntities(e, body);
			const json = await readEnvelope<QueryRow[]>(res);
			expect(json.code).toBe(0);
			expect(json.data).toHaveLength(3);
		});

		it("returns only specified outputFields", async () => {
			const body = {
				collectionName: "query_col",
				filter: 'relativePath == "src/beta.ts"',
				outputFields: ["id", "content"],
			};
			const res = await queryEntities(e, body);
			const json = await readEnvelope<QueryRow[]>(res);
			expect(json.code).toBe(0);
			expect(json.data).toHaveLength(1);
			expect(json.data[0]).toHaveProperty("id", "e2");
			expect(json.data[0]).toHaveProperty("content", "beta content");
			expect(json.data[0]).not.toHaveProperty("relativePath");
			expect(json.data[0]).not.toHaveProperty("startLine");
		});

		it("returns all fields when outputFields is not specified", async () => {
			const body = {
				collectionName: "query_col",
				filter: 'relativePath == "src/beta.ts"',
			};
			const res = await queryEntities(e, body);
			const json = await readEnvelope<QueryRow[]>(res);
			expect(json.code).toBe(0);
			const row = json.data[0];
			expect(row).toHaveProperty("id");
			expect(row).toHaveProperty("content");
			expect(row).toHaveProperty("relativePath");
			expect(row).toHaveProperty("startLine");
			expect(row).toHaveProperty("endLine");
			expect(row).toHaveProperty("fileExtension");
			expect(row).toHaveProperty("metadata");
		});

		it("excludes outputFields that do not exist in the result rows", async () => {
			const body = {
				collectionName: "query_col",
				filter: 'relativePath == "src/beta.ts"',
				outputFields: ["id", "content", "nonExistentField"],
			};
			const res = await queryEntities(e, body);
			const json = await readEnvelope<QueryRow[]>(res);
			expect(json.code).toBe(0);
			expect(json.data).toHaveLength(1);
			expect(json.data[0]).toHaveProperty("id", "e2");
			expect(json.data[0]).toHaveProperty("content", "beta content");
			expect(json.data[0]).not.toHaveProperty("nonExistentField");
		});

		it("returns hydrated rows for non-hybrid collections", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_nohybrid', 3, 0)`,
			);
			for (const id of ["e1", "e2", "e3"]) {
				await trackVectorId("plain_nohybrid", id);
			}

			const getByIdsSpy = vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue([
				{
					id: await physicalId("plain_nohybrid", "e1"),
					namespace: "plain_nohybrid",
					values: [],

```

### 6. test/search.spec.ts (lines 1717-1946)
```ts
			collectionName: "test_col",
			search: [
				{
					data: [[0.1, 0.2, 0.3]],
					annsField: "vector",
					limit: 10,
					filter: 'relativePath like "src/_tmp%%"',
				},
			],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({
				filter: {
					relativePath: { $gte: "src/_tmp%", $lt: "src/_tmp&" },
				},
			}),
		);
	});

	it("forwards top-level body.filter to dense Vectorize query", async () => {
		const querySpy = vi
			.spyOn(e.VECTORIZE, "query")
			.mockResolvedValue(
				await vectorizeMatchesFor("test_col", [MOCK_MATCHES[0]]),
			);

		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
			filter: 'fileExtension == ".ts"',
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({ filter: { fileExtension: { $eq: ".ts" } } }),
		);
	});

	it("applies top-level body.filter to sparse FTS query", async () => {
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches());
		await e.DB.exec(
			`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
		);
		await e.DB.exec(`DROP TABLE IF EXISTS fts_hybrid_col`);
		await e.DB.exec(
			`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
		);
		await e.DB.exec(
			`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('fts1', 'test query content', 'a.ts', 1, 5, '.ts', '{}')`,
		);

		const body = {
			collectionName: "hybrid_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["test query"], annsField: "sparse_vector", limit: 10 },
			],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
			filter: 'fileExtension == ".ts"',
		};

		const dbSpy = vi.spyOn(e.DB, "prepare");
		await hybridSearch(e, body as unknown as MilvusHybridSearchRequest);

		const ftsCalls = dbSpy.mock.calls.filter((c) =>
			String(c[0]).includes("MATCH"),
		);
		expect(ftsCalls.length).toBe(1);
		expect(ftsCalls[0][0]).toContain("AND fileExtension = ?");
	});

	it("prefers sparseReq.filter over top-level body.filter", async () => {
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches());
		await e.DB.exec(
			`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
		);
		await e.DB.exec(`DROP TABLE IF EXISTS fts_hybrid_col`);
		await e.DB.exec(
			`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
		);
		await e.DB.exec(
			`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('fts1', 'test query content', 'a.ts', 1, 5, '.ts', '{}')`,
		);

		const body = {
			collectionName: "hybrid_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{
					data: ["test query"],
					annsField: "sparse_vector",
					limit: 10,
					filter: 'fileExtension == ".ts"',
				},
			],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
			filter: 'fileExtension == ".js"',
		};

		const res = await hybridSearch(
			e,
			body as unknown as MilvusHybridSearchRequest,
		);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(json.data).toHaveLength(1);
		expect(json.data[0]).toEqual(
			expect.objectContaining({ id: "fts1", fileExtension: ".ts" }),
		);
	});

	it("applies numeric filter to sparse FTS query", async () => {
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches());
		await e.DB.exec(
			`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
		);
		await e.DB.exec(`DROP TABLE IF EXISTS fts_hybrid_col`);
		await e.DB.exec(
			`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
		);
		await e.DB.exec(
			`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('fts1', 'test query content', 'a.ts', 1, 5, '.ts', '{}')`,
		);

		const body = {
			collectionName: "hybrid_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["test query"], annsField: "sparse_vector", limit: 10 },
			],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
			filter: "startLine >= 1",
		};

		const dbSpy = vi.spyOn(e.DB, "prepare");
		await hybridSearch(e, body as unknown as MilvusHybridSearchRequest);

		const ftsCalls = dbSpy.mock.calls.filter((c) =>
			String(c[0]).includes("MATCH"),
		);
		expect(ftsCalls.length).toBe(1);
		expect(ftsCalls[0][0]).toContain("AND startLine >= ?");
	});

	it("passes fileExtension in filter from body to Vectorize in hybridSearch", async () => {
		const querySpy = vi
			.spyOn(e.VECTORIZE, "query")
			.mockResolvedValue(
				await vectorizeMatchesFor("test_col", [MOCK_MATCHES[0]]),
			);

		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
			filter: 'fileExtension in ["go", "ts"]',
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({
				filter: { fileExtension: { $in: ["go", "ts"] } },
			}),
		);
	});

	it("applies fileExtension in filter to sparse FTS query with IN clause", async () => {
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches());
		await e.DB.exec(
			`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
		);
		await e.DB.exec(`DROP TABLE IF EXISTS fts_hybrid_col`);
		await e.DB.exec(
			`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
		);
		await e.DB.exec(
			`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('fts1', 'test query content', 'a.ts', 1, 5, 'ts', '{}')`,
		);

		const body = {
			collectionName: "hybrid_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["test query"], annsField: "sparse_vector", limit: 10 },
			],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
			filter: 'fileExtension in ["go", "ts"]',
		};

		const dbSpy = vi.spyOn(e.DB, "prepare");
		await hybridSearch(e, body as MilvusHybridSearchRequest);

		const ftsCalls = dbSpy.mock.calls.filter((c) =>
			String(c[0]).includes("MATCH"),
		);
		expect(ftsCalls.length).toBe(1);
		expect(ftsCalls[0][0]).toContain("AND fileExtension IN (");
	});

	it("applies ancestor-path prefix filters to sparse FTS queries in hybridSearch", async () => {
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches());
		await e.DB.exec(
			`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
		);
		await e.DB.exec(`DROP TABLE IF EXISTS fts_hybrid_col`);
		await e.DB.exec(

```

### 7. src/entities.ts (lines 1-249)
Symbol: rollbackUpsertedVectors (function, lines 109-134)
```ts
import {
	Env,
	EntityData,
	EntityRow,
	MilvusInsertRequest,
	MilvusDeleteRequest,
	MilvusQueryRequest,
	getConfiguredVectorizeDimension,
	ok,
	err,
	validateDefaultDbName,
	toSafeName,
	toPhysicalVectorizeIds,
	ENTITY_FIELDS,
	VectorizeEntityMetadata,
} from "./types";
import { embedTexts } from "./embedding";
import {
	compileFilterPlanToSql,
	isUnsupportedFilterError,
	matchesFilterPlan,
	parseExactIdListFilter,
	parseFilterPlan,
} from "./filters";

const D1_BATCH = 100; // statements per db.batch() call
const DEFAULT_QUERY_LIMIT = 16384;
// D1 enforces a maximum of 100 bound parameters per prepared statement (stricter than SQLite's native 999).
// https://developers.cloudflare.com/d1/platform/limits/
const D1_MAX_BOUND_PARAMS = 100;
const VECTORIZE_GET_BY_IDS_BATCH = 20;
const VECTORIZE_DELETE_BATCH = 100;
const TRACKED_ID_PAGE_SIZE = VECTORIZE_GET_BY_IDS_BATCH;

type TrackedVectorRow = {
	id: string;
	physical_id: string;
};

async function batchedD1Exec(
	db: D1Database,
	stmts: D1PreparedStatement[],
): Promise<void> {
	for (let i = 0; i < stmts.length; i += D1_BATCH) {
		await db.batch(stmts.slice(i, i + D1_BATCH));
	}
}

// Builds multi-row INSERT statements respecting D1's 100-bound-parameter limit per statement.
// sqlPrefix: everything up to (but not including) VALUES, e.g. "INSERT OR REPLACE INTO t (a, b)"
// paramsPerRow: number of bind params per row
// allParams: flat array of all bind values (length must be a multiple of paramsPerRow)
function buildMultiRowInserts(
	db: D1Database,
	sqlPrefix: string,
	paramsPerRow: number,
	allParams: unknown[],
): D1PreparedStatement[] {
	const maxRowsPerStmt = Math.floor(D1_MAX_BOUND_PARAMS / paramsPerRow);
	const rowPlaceholder = `(${new Array(paramsPerRow).fill("?").join(", ")})`;
	const stmts: D1PreparedStatement[] = [];
	for (let i = 0; i < allParams.length; i += maxRowsPerStmt * paramsPerRow) {
		const chunk = allParams.slice(i, i + maxRowsPerStmt * paramsPerRow);
		const rowCount = chunk.length / paramsPerRow;
		const valuePlaceholders = new Array(rowCount)
			.fill(rowPlaceholder)
			.join(", ");
		stmts.push(
			db.prepare(`${sqlPrefix} VALUES ${valuePlaceholders}`).bind(...chunk),
		);
	}
	return stmts;
}

// Builds DELETE ... WHERE id IN (...) statements, chunked to stay within D1_MAX_BOUND_PARAMS.
function buildChunkedInDeletes(
	db: D1Database,
	sqlPrefix: string,
	ids: string[],
	leadingParams: unknown[] = [],
): D1PreparedStatement[] {
	const maxIdsPerStmt = D1_MAX_BOUND_PARAMS - leadingParams.length;
	const stmts: D1PreparedStatement[] = [];
	for (let i = 0; i < ids.length; i += maxIdsPerStmt) {
		const chunk = ids.slice(i, i + maxIdsPerStmt);
		const placeholders = chunk.map(() => "?").join(", ");
		stmts.push(
			db
				.prepare(`${sqlPrefix} (${placeholders})`)
				.bind(...leadingParams, ...chunk),
		);
	}
	return stmts;
}

function extractEntityFields(
	meta: VectorizeEntityMetadata | undefined,
): EntityData {
	return {
		content: meta?.content ?? "",
		relativePath: meta?.relativePath ?? "",
		startLine: meta?.startLine ?? 0,
		endLine: meta?.endLine ?? 0,
		fileExtension: meta?.fileExtension ?? "",
		metadata: meta?.metadata ?? "{}",
	};
}

async function rollbackUpsertedVectors(
	env: Env,
	collectionName: string,
	physicalIds: string[],
	originalError: unknown,
): Promise<void> {
	try {
		await deletePhysicalVectorIds(env, physicalIds);
	} catch (rollbackError) {
		console.error(
			JSON.stringify({
				level: "error",
				operation: "vectorize_insert_rollback",
				collectionName,
				originalError:
					originalError instanceof Error
						? originalError.message
						: String(originalError),
				rollbackError:
					rollbackError instanceof Error
						? rollbackError.message
						: String(rollbackError),
			}),
		);
	}
}

async function deletePhysicalVectorIds(
	env: Env,
	physicalIds: string[],
): Promise<void> {
	for (let i = 0; i < physicalIds.length; i += VECTORIZE_DELETE_BATCH) {
		await env.VECTORIZE.deleteByIds(
			physicalIds.slice(i, i + VECTORIZE_DELETE_BATCH),
		);
	}
}

export async function loadVectorRecordsByPhysicalIds(
	env: Env,
	physicalIds: string[],
	safeName?: string,
): Promise<VectorizeVector[]> {
	const vectors: VectorizeVector[] = [];

	for (let i = 0; i < physicalIds.length; i += VECTORIZE_GET_BY_IDS_BATCH) {
		const chunk = physicalIds.slice(i, i + VECTORIZE_GET_BY_IDS_BATCH);
		const records = await env.VECTORIZE.getByIds(chunk);
		for (const vector of records) {
			if (safeName && vector.namespace && vector.namespace !== safeName)
				continue;
			vectors.push(vector);
		}
	}

	return vectors;
}

async function restoreVectorRecords(
	env: Env,
	vectors: VectorizeVector[],
): Promise<void> {
	for (let i = 0; i < vectors.length; i += 1000) {
		await env.VECTORIZE.upsert(vectors.slice(i, i + 1000));
	}
}

export async function rollbackDeletedVectors(
	env: Env,
	collectionName: string,
	vectors: VectorizeVector[],
	originalError: unknown,
): Promise<void> {
	try {
		await restoreVectorRecords(env, vectors);
	} catch (rollbackError) {
		console.error(
			JSON.stringify({
				level: "error",
				operation: "vectorize_delete_rollback",
				collectionName,
				originalError:
					originalError instanceof Error
						? originalError.message
						: String(originalError),
				rollbackError:
					rollbackError instanceof Error
						? rollbackError.message
						: String(rollbackError),
			}),
		);
	}
}

async function loadTrackedPhysicalVectorIds(
	env: Env,
	collectionName: string,
	logicalIds: string[],
): Promise<string[]> {
	const physicalIds: string[] = [];
	const maxIdsPerStmt = D1_MAX_BOUND_PARAMS - 1;

	for (let offset = 0; offset < logicalIds.length; offset += maxIdsPerStmt) {
		const ids = logicalIds.slice(offset, offset + maxIdsPerStmt);
		const placeholders = ids.map(() => "?").join(", ");
		const { results } = await env.DB.prepare(
			`SELECT id, physical_id FROM collection_vectors WHERE collection_name = ? AND id IN (${placeholders})`,
		)
			.bind(collectionName, ...ids)
			.run<TrackedVectorRow>();
		const physicalIdByLogicalId = new Map(
			results.map((row) => [row.id, row.physical_id]),
		);

		for (const id of ids) {
			const physicalId = physicalIdByLogicalId.get(id);
			if (physicalId) physicalIds.push(physicalId);
		}
	}

	return physicalIds;
}

async function loadTrackedVectorRecords(
	env: Env,
	collectionName: string,
	logicalIds: string[],
): Promise<VectorizeVector[]> {
	if (logicalIds.length === 0) return [];
	const physicalIds = await loadTrackedPhysicalVectorIds(
		env,
		collectionName,
		logicalIds,
	);
	if (physicalIds.length === 0) return [];
	return loadVectorRecordsByPhysicalIds(
		env,
		physicalIds,
		toSafeName(collectionName),
	);
}


```

### 8. test/entities.spec.ts (lines 1804-2086)
```ts
		it("returns hydrated rows for non-hybrid collections", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_nohybrid', 3, 0)`,
			);
			for (const id of ["e1", "e2", "e3"]) {
				await trackVectorId("plain_nohybrid", id);
			}

			const getByIdsSpy = vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue([
				{
					id: await physicalId("plain_nohybrid", "e1"),
					namespace: "plain_nohybrid",
					values: [],
					metadata: {
						content: "alpha content",
						relativePath: "src/alpha.ts",
						startLine: 1,
						endLine: 10,
						fileExtension: ".ts",
						metadata: "{}",
					},
				},
				{
					id: await physicalId("plain_nohybrid", "e2"),
					namespace: "plain_nohybrid",
					values: [],
					metadata: {
						content: "beta content",
						relativePath: "src/beta.ts",
						startLine: 11,
						endLine: 20,
						fileExtension: ".ts",
						metadata: '{"lang":"ts"}',
					},
				},
				{
					id: await physicalId("plain_nohybrid", "e3"),
					namespace: "plain_nohybrid",
					values: [],
					metadata: {
						content: "gamma content",
						relativePath: "src/gamma.js",
						startLine: 21,
						endLine: 30,
						fileExtension: ".js",
						metadata: '{"lang":"js"}',
					},
				},
			] as unknown as VectorizeVector[]);

			const res = await queryEntities(e, {
				collectionName: "plain_nohybrid",
				filter: "",
				limit: 2,
			});
			const json = await readEnvelope<QueryRow[]>(res);

			expect(json.code).toBe(0);
			expect(getByIdsSpy).toHaveBeenCalledWith([
				await physicalId("plain_nohybrid", "e1"),
				await physicalId("plain_nohybrid", "e2"),
				await physicalId("plain_nohybrid", "e3"),
			]);
			expect(json.data).toHaveLength(2);
			expect(json.data).toEqual([
				expect.objectContaining({ id: "e1", content: "alpha content" }),
				expect.objectContaining({ id: "e2", content: "beta content" }),
			]);
		});

		it("returns an empty result for non-hybrid collections with no tracked ids", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_empty', 3, 0)`,
			);
			const getByIdsSpy = vi.spyOn(e.VECTORIZE, "getByIds");

			const res = await queryEntities(e, {
				collectionName: "plain_empty",
				filter: "",
				limit: 5,
			});
			const json = await readEnvelope<QueryRow[]>(res);

			expect(json.code).toBe(0);
			expect(json.data).toEqual([]);
			expect(getByIdsSpy).not.toHaveBeenCalled();
		});


...5495 bytes truncated...

The tool call succeeded but the output was truncated. Full output saved to: /home/jaryl/.local/share/opencode/tool-output/tool_d052443b20015JhglPNkuca1F5
Use the Task tool to have explore agent process this file with Grep and Read (with offset/limit). Do NOT read the full file yourself - delegate to save context.
```

**Tool: cfmantic-code_search_code**

**Input:**
```json
{
  "path": "/home/jaryl/Projects/cf-workers-milvus",
  "query": "how does collection drop restore deleted vectors",
  "limit": 8
}
```

**Output:**
```
Found 8 results for "how does collection drop restore deleted vectors":

### 1. test/collections.spec.ts (lines 592-874)
```ts
							relativePath: `${id}.ts`,
							startLine: 1,
							endLine: 2,
							fileExtension: ".ts",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			vectors.filter((vector) => ids.includes(vector.id)),
		);
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		const upsertSpy = vi.spyOn(e.VECTORIZE, "upsert").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.DB, "batch").mockRejectedValueOnce(
			new Error("D1 cleanup failed"),
		);

		await expect(
			dropCollection(e, { collectionName: "rollback_drop" }),
		).rejects.toThrow("D1 cleanup failed");
		expect(e.VECTORIZE.deleteByIds).toHaveBeenCalledWith([
			await physicalId("rollback_drop", "v1"),
			await physicalId("rollback_drop", "v2"),
		]);
		expect(upsertSpy).toHaveBeenCalledWith(vectors);
	});

	it("skips mismatched namespaces and logs failed rollback restores", async () => {
		await createCollection(e, {
			collectionName: "rollback_mismatch",
			schema: validSchema(128, true),
		});
		await trackVectorId("rollback_mismatch", "v1");
		await trackVectorId("rollback_mismatch", "v2");

		const vectors = await Promise.all(
			["v1", "v2"].map(
				async (id) =>
					({
						id: await physicalId("rollback_mismatch", id),
						values: [0.1, 0.2, 0.3],
						namespace: id === "v1" ? "other_namespace" : "rollback_mismatch",
						metadata: {
							content: id,
							relativePath: `${id}.ts`,
							startLine: 1,
							endLine: 2,
							fileExtension: ".ts",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			vectors.filter((vector) => ids.includes(vector.id)),
		);
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.VECTORIZE, "upsert").mockRejectedValueOnce(
			new Error("rollback restore failed"),
		);
		const consoleErrorSpy = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});
		vi.spyOn(e.DB, "batch").mockRejectedValueOnce(
			new Error("D1 cleanup failed"),
		);

		await expect(
			dropCollection(e, { collectionName: "rollback_mismatch" }),
		).rejects.toThrow("D1 cleanup failed");
		expect(e.VECTORIZE.deleteByIds).toHaveBeenCalledWith([
			await physicalId("rollback_mismatch", "v1"),
			await physicalId("rollback_mismatch", "v2"),
		]);
		expect(consoleErrorSpy).toHaveBeenCalled();
	});

	it("does not attempt rollback when drop cleanup fails before any vectors are recovered", async () => {
		await createCollection(e, {
			collectionName: "rollback_empty",
			schema: validSchema(128, true),
		});
		await trackVectorId("rollback_empty", "v1");

		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue([]);
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		const upsertSpy = vi.spyOn(e.VECTORIZE, "upsert").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		const consoleErrorSpy = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});
		vi.spyOn(e.DB, "batch").mockRejectedValueOnce(
			new Error("D1 cleanup failed"),
		);

		await expect(
			dropCollection(e, { collectionName: "rollback_empty" }),
		).rejects.toThrow("D1 cleanup failed");
		expect(upsertSpy).not.toHaveBeenCalled();
		expect(consoleErrorSpy).not.toHaveBeenCalled();
	});

	it("limits concurrent Vectorize deletes while dropping a collection", async () => {
		await seedCollection("bounded_drop");
		const ids = Array.from({ length: 550 }, (_, i) => `v${i}`);
		for (const id of ids) {
			await trackVectorId("bounded_drop", id);
		}

		const resolvers: Array<() => void> = [];
		const deleteSpy = vi.spyOn(e.VECTORIZE, "deleteByIds").mockImplementation(
			() =>
				new Promise((resolve) => {
					resolvers.push(() =>
						resolve({
							ids: [],
							count: 0,
							mutationId: "mock",
						} as VectorMutation),
					);
				}),
		);
		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			ids.map(
				(id) =>
					({
						id,
						values: [0.1, 0.2, 0.3],
						namespace: "bounded_drop",
						metadata: {
							content: "",
							relativePath: "",
							startLine: 0,
							endLine: 0,
							fileExtension: "",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		const dropPromise = dropCollection(e, { collectionName: "bounded_drop" });

		const initialConcurrency = 5;
		await vi.waitFor(() =>
			expect(deleteSpy).toHaveBeenCalledTimes(initialConcurrency),
		);

		for (const resolve of resolvers.splice(0, initialConcurrency)) resolve();

		await vi.waitFor(() => expect(deleteSpy).toHaveBeenCalledTimes(6));
		resolvers.splice(0).forEach((resolve) => resolve());

		const json = await (await dropPromise).json<{ code: number }>();
		expect(json.code).toBe(0);
	});

	it("pages tracked vector IDs while dropping a collection", async () => {
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			ids.map(
				(id) =>
					({
						id,
						values: [0.1, 0.2, 0.3],
						namespace: "paged_drop",
						metadata: {
							content: "",
							relativePath: "",
							startLine: 0,
							endLine: 0,
							fileExtension: "",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		await seedCollection("paged_drop");
		const ids = Array.from({ length: 501 }, (_, i) => `v${i}`);
		for (const id of ids) {
			await trackVectorId("paged_drop", id);
		}

		const prepareSpy = vi.spyOn(e.DB, "prepare");

		const res = await dropCollection(e, { collectionName: "paged_drop" });
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(0);

		const trackedIdSelects = prepareSpy.mock.calls
			.map(([sql]) => sql)
			.filter((sql) =>
				sql.startsWith(
					"SELECT physical_id FROM collection_vectors WHERE collection_name = ?",
				),
			);

		expect(trackedIdSelects).toHaveLength(2);
		expect(trackedIdSelects.every((sql) => sql.includes("LIMIT"))).toBe(true);
	});

	it("deletes Vectorize vectors for non-hybrid collections using collection_vectors tracking", async () => {
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
			ids: [],
			count: 0,
			mutationId: "mock",
		} as VectorMutation);
		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			ids.map(
				(id) =>
					({
						id,
						values: [0.1, 0.2, 0.3],
						namespace: "non_hybrid_col",
						metadata: {
							content: "",
							relativePath: "",
							startLine: 0,
							endLine: 0,
							fileExtension: "",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		await seedCollection("non_hybrid_col", 128, 0);
		await trackVectorId("non_hybrid_col", "v1");
		await trackVectorId("non_hybrid_col", "v2");

		const res = await dropCollection(e, { collectionName: "non_hybrid_col" });
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(0);
		expect(e.VECTORIZE.deleteByIds).toHaveBeenCalledWith([
			await physicalId("non_hybrid_col", "v1"),
			await physicalId("non_hybrid_col", "v2"),
		]);

		// collection_vectors should be cleaned up
		const { results } = await e.DB.prepare(
			"SELECT * FROM collection_vectors WHERE collection_name = ?",
		)
			.bind("non_hybrid_col")
			.run();
		expect(results).toHaveLength(0);

		// collection row should be deleted
		const row = await e.DB.prepare("SELECT * FROM collections WHERE name = ?")
			.bind("non_hybrid_col")
			.first();
		expect(row).toBeNull();
	});

	it("returns ok gracefully when the collection does not exist", async () => {

```

### 2. test/conventions.spec.ts (lines 68-331)
```ts
// ─── Tests ────────────────────────────────────────────────────────────────────

describe("Milvus envelope convention", () => {
	beforeEach(async () => {
		e.VECTORIZE_DIMENSION = "128";
		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collections (name TEXT PRIMARY KEY, safe_name TEXT UNIQUE, dimension INTEGER NOT NULL, is_hybrid INTEGER NOT NULL DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()))`,
		);
		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collection_vectors (id TEXT NOT NULL, physical_id TEXT NOT NULL UNIQUE, collection_name TEXT NOT NULL, PRIMARY KEY (id, collection_name))`,
		);
		await e.DB.exec(`DELETE FROM collections`);
		await e.DB.exec(`DELETE FROM collection_vectors`);
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	describe("non-POST method", () => {
		it("GET request returns valid error envelope with HTTP 200", async () => {
			const res = await fetchWorker(
				new Req("http://localhost/v2/vectordb/collections/list", {
					method: "GET",
				}),
				e,
			);
			const json = await res.json<Envelope>();
			assertEnvelope(res, json);
			expect(json.code).toBeGreaterThan(0);
		});

		it("PUT request returns valid error envelope with HTTP 200", async () => {
			const res = await fetchWorker(
				new Req("http://localhost/v2/vectordb/collections/create", {
					method: "PUT",
				}),
				e,
			);
			const json = await res.json<Envelope>();
			assertEnvelope(res, json);
		});
	});

	describe("auth failures", () => {
		it("missing Authorization header returns valid error envelope", async () => {
			const res = await fetchWorker(
				new Req("http://localhost/v2/vectordb/collections/list", {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: "{}",
				}),
				e,
			);
			const json = await res.json<Envelope>();
			assertEnvelope(res, json);
			expect(json.code).toBe(1800);
		});

		it("wrong token returns valid error envelope", async () => {
			const res = await fetchWorker(
				new Req("http://localhost/v2/vectordb/collections/list", {
					method: "POST",
					headers: {
						"Content-Type": "application/json",
						Authorization: "Bearer wrong-token",
					},
					body: "{}",
				}),
				e,
			);
			const json = await res.json<Envelope>();
			assertEnvelope(res, json);
			expect(json.code).toBe(1800);
		});
	});

	describe("bad request body", () => {
		it("invalid JSON returns valid error envelope", async () => {
			const res = await fetchWorker(
				new Req("http://localhost/v2/vectordb/collections/list", {
					method: "POST",
					headers: {
						"Content-Type": "application/json",
						Authorization: `Bearer ${e.AUTH_TOKEN}`,
					},
					body: "{ not: valid json }}}",
				}),
				e,
			);
			const json = await res.json<Envelope>();
			assertEnvelope(res, json);
			expect(json.message).toBe("Invalid JSON request body");
		});

		it("empty body returns valid error envelope", async () => {
			const res = await fetchWorker(
				new Req("http://localhost/v2/vectordb/collections/list", {
					method: "POST",
					headers: {
						"Content-Type": "application/json",
						Authorization: `Bearer ${e.AUTH_TOKEN}`,
					},
					body: "",
				}),
				e,
			);
			const json = await res.json<Envelope>();
			assertEnvelope(res, json);
		});
	});

	describe("unknown routes", () => {
		it("/v2/unknown returns valid error envelope", async () => {
			const { res, json } = await call("/v2/unknown");
			assertEnvelope(res, json);
		});

		it("/v2/vectordb/bogus/path returns valid error envelope", async () => {
			const { res, json } = await call("/v2/vectordb/bogus/path");
			assertEnvelope(res, json);
		});

		it("/ (root) returns valid error envelope", async () => {
			const { res, json } = await call("/");
			assertEnvelope(res, json);
		});
	});

	describe("collection CRUD success paths", () => {
		it("/collections/list returns valid success envelope with array data", async () => {
			const { res, json } = await call("/v2/vectordb/collections/list");
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
			expect(Array.isArray(json.data)).toBe(true);
		});

		it("/collections/has (missing) returns valid success envelope", async () => {
			const { res, json } = await call("/v2/vectordb/collections/has", {
				collectionName: "nope",
			});
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/collections/has (existing) returns valid success envelope", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('exists', 128, 0)`,
			);
			const { res, json } = await call("/v2/vectordb/collections/has", {
				collectionName: "exists",
			});
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/collections/get_load_state (not loaded) returns valid success envelope", async () => {
			const { res, json } = await call(
				"/v2/vectordb/collections/get_load_state",
				{ collectionName: "ghost" },
			);
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/collections/get_load_state (loaded) returns valid success envelope", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('loaded', 128, 0)`,
			);
			const { res, json } = await call(
				"/v2/vectordb/collections/get_load_state",
				{ collectionName: "loaded" },
			);
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/collections/create (valid schema) returns valid success envelope", async () => {
			const body = {
				collectionName: "test_col",
				schema: {
					fields: [
						{
							fieldName: "vec",
							dataType: "FloatVector",
							elementTypeParams: { dim: 128 },
						},
					],
				},
			};
			const { res, json } = await call("/v2/vectordb/collections/create", body);
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/collections/create (idempotent, already exists) returns valid success envelope", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('dup_col', 128, 0)`,
			);
			const body = {
				collectionName: "dup_col",
				schema: {
					fields: [
						{
							fieldName: "vec",
							dataType: "FloatVector",
							elementTypeParams: { dim: 128 },
						},
					],
				},
			};
			const { res, json } = await call("/v2/vectordb/collections/create", body);
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/collections/drop returns valid success envelope", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('drop_me', 128, 0)`,
			);
			vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue({
				ids: [],
				count: 0,
				mutationId: "mock",
			} as VectorMutation);
			const { res, json } = await call("/v2/vectordb/collections/drop", {
				collectionName: "drop_me",
			});
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/indexes/create returns valid success envelope", async () => {
			const { res, json } = await call("/v2/vectordb/indexes/create", {
				collectionName: "any",
			});
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});

		it("/collections/load returns valid success envelope", async () => {
			const { res, json } = await call("/v2/vectordb/collections/load", {
				collectionName: "any",
			});
			assertEnvelope(res, json);
			expect(json.code).toBe(0);
		});
	});

	describe("collection CRUD error paths", () => {
		it("/collections/list rejects unsupported dbName with a valid error envelope", async () => {
			const { res, json } = await call("/v2/vectordb/collections/list", {
				dbName: "tenant_a",
			});
			assertEnvelope(res, json);
			expect(json.code).toBe(1);
			expect(json.message).toMatch(/unsupported dbname/i);
		});

		it("/collections/create (no FloatVector field) returns valid error envelope", async () => {
			const body = {
				collectionName: "bad",
				schema: { fields: [{ fieldName: "id", dataType: "VarChar" }] },
			};

```

### 3. test/search.spec.ts (lines 1013-1246)
```ts
			annsField: "vector",
			limit: 10,
			filter: 'unknownField == "x"',
		};

		const res = await searchEntities(e, body as MilvusSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/unsupported filter/i);
		expect(querySpy).not.toHaveBeenCalled();
	});
});

describe("hybridSearch", () => {
	beforeEach(async () => {
		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collections (name TEXT PRIMARY KEY, safe_name TEXT UNIQUE, dimension INTEGER NOT NULL, is_hybrid INTEGER NOT NULL DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()))`,
		);
		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collection_vectors (id TEXT NOT NULL, physical_id TEXT NOT NULL UNIQUE, collection_name TEXT NOT NULL, PRIMARY KEY (id, collection_name))`,
		);
		await e.DB.exec(`DELETE FROM collections`);
		await e.DB.exec(`DELETE FROM collection_vectors`);
		await seedDefaultTrackedIds();
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("returns error code 1 when the dense search request (annsField=vector) is missing", async () => {
		const body = {
			collectionName: "test_col",
			search: [{ data: ["hello world"], annsField: "sparse_vector", limit: 5 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		};

		const res = await hybridSearch(
			e,
			body as unknown as MilvusHybridSearchRequest,
		);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/Missing dense search request/i);
	});

	it("returns error code 1 when the dense search request data array is empty", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		const body = {
			collectionName: "test_col",
			search: [{ data: [], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toBe("Missing query vector in data[0]");
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("returns error when the dense query vector length does not match the configured dimension", async () => {
		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2]], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/dimension/i);
	});

	it("returns a deterministic error when dense embedding lookup returns an empty response", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		vi.spyOn(e.AI, "run").mockResolvedValue({
			data: [],
			shape: [0, 0],
		} as unknown as RerankRunOutput);

		const res = await hybridSearch(e, {
			collectionName: "test_col",
			search: [{ data: ["hello"], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		} as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toBe(
			"Invalid embedding response: missing query vector",
		);
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("rethrows unexpected embedQuery failures during hybridSearch", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		vi.spyOn(embedding, "embedQuery").mockRejectedValueOnce(
			new Error("embed boom"),
		);

		await expect(
			hybridSearch(e, {
				collectionName: "test_col",
				search: [{ data: ["hello"], annsField: "vector", limit: 10 }],
				rerank: { strategy: "rrf", params: { k: 60 } },
				limit: 10,
			} as MilvusHybridSearchRequest),
		).rejects.toThrow("embed boom");
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("returns an error when the top-level limit is not a finite positive integer", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 0,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/invalid limit/i);
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("returns an error when denseReq.limit is not a finite positive integer", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 0 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/dense search limit/i);
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("returns an error when sparseReq.limit is not a finite positive integer", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 0 },
			],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/sparse search limit/i);
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("returns an error when rerank.params.k is not a finite positive integer", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 0 } },
			limit: 10,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/rerank\.params\.k/i);
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("returns fused dense results when no sparse search request is provided", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			vectorizeMatches([
				vectorizeMatch({
					id: await physicalId("test_col", "id1"),
					score: 0.95,
					metadata: MOCK_MATCHES[0].metadata as Record<string, unknown>,
				}),
				vectorizeMatch({
					id: await physicalId("test_col", "id2"),
					score: 0.8,
					metadata: MOCK_MATCHES[1].metadata as Record<string, unknown>,
				}),
			]),
		);
		const getByIdsSpy = vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue([]);

		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 }],
			rerank: { strategy: "rrf", params: { k: 60 } },
			limit: 10,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({ topK: 10, returnMetadata: "all" }),
		);
		expect(getByIdsSpy).not.toHaveBeenCalled();
		expect(json.data).toHaveLength(2);
		expect(json.data[0]).toMatchObject({ id: "id1" });
		expect(json.data[1]).toMatchObject({ id: "id2" });
	});

	it("uses ids-only dense Vectorize queries for true hybrid requests even under the metadata cap", async () => {
		await createFtsTable("test_col");
		const querySpy = vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			vectorizeMatches([
				vectorizeMatch({

```

### 4. test/entities.spec.ts (lines 132-353)
```ts
describe("entities", () => {
	beforeEach(async () => {
		e.VECTORIZE_DIMENSION = "3";
		vi.spyOn(e.VECTORIZE, "upsert").mockResolvedValue(VECTORIZE_MUTATION);
		vi.spyOn(e.VECTORIZE, "deleteByIds").mockResolvedValue(VECTORIZE_MUTATION);
		vi.spyOn(e.VECTORIZE, "getByIds").mockImplementation(async (ids) =>
			ids.map(
				(id) =>
					({
						id,
						values: [0.1, 0.2, 0.3],
						metadata: {
							content: "",
							relativePath: "",
							startLine: 0,
							endLine: 0,
							fileExtension: "",
							metadata: "{}",
						},
					}) as VectorizeVector,
			),
		);

		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collections (name TEXT PRIMARY KEY, safe_name TEXT UNIQUE, dimension INTEGER NOT NULL, is_hybrid INTEGER NOT NULL DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()))`,
		);
		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collection_vectors (id TEXT NOT NULL, physical_id TEXT NOT NULL UNIQUE, collection_name TEXT NOT NULL, PRIMARY KEY (id, collection_name))`,
		);
		await e.DB.exec(`DELETE FROM collections`);
		await e.DB.exec(`DELETE FROM collection_vectors`);
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	describe("dbName validation", () => {
		it("rejects non-default dbName for entity operations", async () => {
			const insertRes = await insertEntities(e, {
				collectionName: "db_guard",
				dbName: "tenant",
				data: [makeEntity("e1")],
			} as never);
			const insertJson = await readEnvelope<MutationData>(insertRes);
			expect(insertJson.code).toBe(1);
			expect(insertJson.message).toMatch(/Unsupported dbName/i);

			const deleteRes = await deleteEntities(e, {
				collectionName: "db_guard",
				dbName: "tenant",
				filter: 'id in ["e1"]',
			} as never);
			const deleteJson = await readEnvelope<MutationData>(deleteRes);
			expect(deleteJson.code).toBe(1);
			expect(deleteJson.message).toMatch(/Unsupported dbName/i);

			const queryRes = await queryEntities(e, {
				collectionName: "db_guard",
				dbName: "tenant",
				filter: 'id in ["e1"]',
				limit: 1,
			} as never);
			const queryJson = await readEnvelope<MutationData>(queryRes);
			expect(queryJson.code).toBe(1);
			expect(queryJson.message).toMatch(/Unsupported dbName/i);
		});
	});

	describe("insertEntities", () => {
		it("returns error code 1800 when collection is not found", async () => {
			const body = { collectionName: "missing_col", data: [makeEntity("e1")] };
			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(1800);
			expect(json.message).toContain("collection not found");
		});

		it("upserts vectors via Vectorize for a non-hybrid collection", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const body = {
				collectionName: "plain_col",
				data: [makeEntity("e1"), makeEntity("e2")],
			};
			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);
			expect(json.data.insertCount).toBe(2);
			expect(json.data.insertIds).toEqual(["e1", "e2"]);
			expect(e.VECTORIZE.upsert).toHaveBeenCalledOnce();
			expect(firstInsertedVector().id).toBe(
				await physicalId("plain_col", "e1"),
			);
		});

		it("returns error when a vector length does not match the configured dimension", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const body = {
				collectionName: "plain_col",
				data: [{ ...makeEntity("e1"), vector: [0.1, 0.2] }],
			};

			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);

			expect(json.code).toBe(1);
			expect(json.message).toMatch(/dimension/i);
			expect(e.VECTORIZE.upsert).not.toHaveBeenCalled();
		});

		it("returns error when the stored collection dimension does not match the configured Vectorize dimension", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 2, 0)`,
			);
			const body = {
				collectionName: "plain_col",
				data: [makeEntity("e1")],
			};

			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);

			expect(json.code).toBe(1);
			expect(json.message).toMatch(/configured Vectorize dimension/i);
			expect(e.VECTORIZE.upsert).not.toHaveBeenCalled();
		});

		it("batches Vectorize upserts in chunks of 1000", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('big_col', 3, 0)`,
			);
			const entities = Array.from({ length: 1500 }, (_, i) =>
				makeEntity(`e${i}`),
			);
			const body = { collectionName: "big_col", data: entities };
			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);
			expect(json.data.insertCount).toBe(1500);
			expect(e.VECTORIZE.upsert).toHaveBeenCalledTimes(2);
		});

		it("inserts into FTS table for a hybrid collection", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
			);
			await e.DB.exec(
				`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
			);
			await e.DB.exec(`DELETE FROM fts_hybrid_col`);

			const entity = makeEntity("e1", {
				content: "hello world",
				relativePath: "src/foo.ts",
			});
			const body = { collectionName: "hybrid_col", data: [entity] };
			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);
			expect(json.data.insertCount).toBe(1);

			const { results } = await e.DB.prepare(
				"SELECT * FROM fts_hybrid_col",
			).run();
			expect(results).toHaveLength(1);
			expect(results[0].id).toBe("e1");
			expect(results[0].content).toBe("hello world");
			expect(results[0].relativePath).toBe("src/foo.ts");
		});

		it("inserts all entities into FTS table via bulk statements", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
			);
			await e.DB.exec(
				`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
			);
			await e.DB.exec(`DELETE FROM fts_hybrid_col`);

			const entities = Array.from({ length: 35 }, (_, i) =>
				makeEntity(`e${i}`),
			);
			const body = { collectionName: "hybrid_col", data: entities };
			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);

			const { results } = await e.DB.prepare(
				"SELECT COUNT(*) as cnt FROM fts_hybrid_col",
			).run<CountRow>();
			expect(results[0].cnt).toBe(35);
		});

		it("does not insert into FTS for a non-hybrid collection", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const body = { collectionName: "plain_col", data: [makeEntity("e1")] };
			await insertEntities(e, body);

			// No FTS table should exist for plain_col
			const table = await e.DB.prepare(
				"SELECT name FROM sqlite_master WHERE type='table' AND name='fts_plain_col'",
			).first();
			expect(table).toBeNull();
		});

		it("succeeds when content exceeds 9000 bytes (truncation applied internally)", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const largeContent = "x".repeat(10_000);
			const body = {
				collectionName: "plain_col",
				data: [makeEntity("e1", { content: largeContent })],
			};
			const res = await insertEntities(e, body);
			const json = await readEnvelope<MutationData>(res);

```

### 5. test/entities.spec.ts (lines 1402-1604)
```ts
			const { results } = await e.DB.prepare(
				"SELECT * FROM fts_hybrid_col",
			).run();
			expect(results).toHaveLength(1);
		});

		it("removes tracked vector IDs from collection_vectors on delete by ID", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			await trackVectorId("plain_col", "id1");
			await trackVectorId("plain_col", "id2");
			await trackVectorId("plain_col", "id3");

			const body = {
				collectionName: "plain_col",
				filter: 'id in ["id1","id2"]',
			};
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);

			const { results } = await e.DB.prepare(
				"SELECT id FROM collection_vectors WHERE collection_name = ?",
			)
				.bind("plain_col")
				.run<{ id: string }>();
			expect(results.map((r) => r.id)).toEqual(["id3"]);
		});

		it("handles id filter with empty brackets (no quoted IDs)", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const body = { collectionName: "plain_col", filter: "id in []" };
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);
			expect(e.VECTORIZE.deleteByIds).not.toHaveBeenCalled();
		});

		it("returns an explicit error for empty non-id delete filters", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const body = { collectionName: "plain_col", filter: "   " };
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(1);
			expect(json.message).toBe("Unsupported filter expression: empty filter");
			expect(e.VECTORIZE.deleteByIds).not.toHaveBeenCalled();
		});

		it("returns an error for malformed id filters", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const body = {
				collectionName: "plain_col",
				filter: "id in [unquoted_value]",
			};
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(1);
			expect(json.message).toMatch(/unsupported filter/i);
		});

		it("propagates FTS error for hybrid collection on ID-based delete (no FTS table)", async () => {
			// hybrid collection exists but FTS table was never created
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_no_fts', 3, 1)`,
			);
			await e.DB.prepare(
				`INSERT INTO collection_vectors (id, physical_id, collection_name) VALUES (?, ?, ?)`,
			)
				.bind("id1", await physicalId("hybrid_no_fts", "id1"), "hybrid_no_fts")
				.run();

			const body = { collectionName: "hybrid_no_fts", filter: 'id in ["id1"]' };
			await expect(deleteEntities(e, body)).rejects.toThrow();

			const { results } = await e.DB.prepare(
				"SELECT id FROM collection_vectors WHERE collection_name = ?",
			)
				.bind("hybrid_no_fts")
				.run<{ id: string }>();
			expect(results.map((row) => row.id)).toEqual(["id1"]);
		});

		it("returns error when relativePath filter is used on non-hybrid collection", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('plain_col', 3, 0)`,
			);
			const body = {
				collectionName: "plain_col",
				filter: 'relativePath == "src/test.ts"',
			};
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(1);
			expect(json.message).toContain("hybrid");
		});

		it("deletes by startLine >= filter by querying FTS for matching IDs", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
			);
			await e.DB.exec(
				`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
			);
			await e.DB.exec(`DELETE FROM fts_hybrid_col`);
			await e.DB.prepare(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			)
				.bind("e1", "content1", "src/a.ts", 1, 10, ".ts", "{}")
				.run();
			await e.DB.prepare(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			)
				.bind("e2", "content2", "src/b.ts", 11, 20, ".ts", "{}")
				.run();
			await trackVectorId("hybrid_col", "e2");

			// startLine >= 10 should match e2 (startLine=11), keep e1 (startLine=1)
			const body = { collectionName: "hybrid_col", filter: "startLine >= 10" };
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);
			expect(e.VECTORIZE.deleteByIds).toHaveBeenCalled();

			const { results } = await e.DB.prepare(
				"SELECT * FROM fts_hybrid_col",
			).run();
			expect(results).toHaveLength(1);
			expect(results[0].id).toBe("e1");
		});

		it("deletes by endLine == filter (numeric equality) by querying FTS for matching IDs", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
			);
			await e.DB.exec(
				`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
			);
			await e.DB.exec(`DELETE FROM fts_hybrid_col`);
			await e.DB.prepare(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			)
				.bind("e1", "content1", "src/a.ts", 1, 10, ".ts", "{}")
				.run();
			await e.DB.prepare(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			)
				.bind("e2", "content2", "src/b.ts", 1, 20, ".ts", "{}")
				.run();
			await trackVectorId("hybrid_col", "e2");

			// endLine == 20 matches e2, keeps e1
			const body = { collectionName: "hybrid_col", filter: "endLine == 20" };
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).toBe(0);
			expect(e.VECTORIZE.deleteByIds).toHaveBeenCalled();

			const { results } = await e.DB.prepare(
				"SELECT * FROM fts_hybrid_col",
			).run();
			expect(results).toHaveLength(1);
			expect(results[0].id).toBe("e1");
		});

		it("returns error for unknown field name in delete filter", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
			);
			const body = {
				collectionName: "hybrid_col",
				filter: 'unknownField == "x"',
			};
			const res = await deleteEntities(e, body);
			const json = await readEnvelope<MutationData>(res);
			expect(json.code).not.toBe(0);
			expect(json.message).toMatch(/unsupported filter/i);
		});

		it("deletes entities matching a fileExtension in [...] filter", async () => {
			await e.DB.exec(
				`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
			);
			await e.DB.exec(
				`CREATE VIRTUAL TABLE IF NOT EXISTS fts_hybrid_col USING fts5(id UNINDEXED, content, relativePath UNINDEXED, startLine UNINDEXED, endLine UNINDEXED, fileExtension UNINDEXED, metadata UNINDEXED)`,
			);
			await e.DB.exec(`DELETE FROM fts_hybrid_col`);

			for (const [id, ext] of [
				["e1", "ts"],
				["e2", "go"],
				["e3", "py"],
			]) {
				await e.DB.prepare(
					`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				)
					.bind(id, `${ext} content`, `src/file.${ext}`, 1, 10, ext, "{}")

```

### 6. test/collections.spec.ts (lines 79-332)
```ts
// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

beforeEach(async () => {
	e.VECTORIZE_DIMENSION = String(CONFIGURED_VECTOR_DIMENSION);
	await e.DB.exec(
		`CREATE TABLE IF NOT EXISTS collections (name TEXT PRIMARY KEY, safe_name TEXT UNIQUE, dimension INTEGER NOT NULL, is_hybrid INTEGER NOT NULL DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()))`,
	);
	await e.DB.exec(
		`CREATE TABLE IF NOT EXISTS collection_vectors (id TEXT NOT NULL, physical_id TEXT NOT NULL UNIQUE, collection_name TEXT NOT NULL, PRIMARY KEY (id, collection_name))`,
	);
	await e.DB.exec(`DELETE FROM collections`);
	await e.DB.exec(`DELETE FROM collection_vectors`);

	// Drop any FTS virtual tables left behind by previous tests
	const { results } = await e.DB.prepare(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'fts_%'`,
	).run<{
		name: string;
	}>();

	for (const { name } of results) {
		await e.DB.exec(`DROP TABLE IF EXISTS ${name}`);
	}
});

// ---------------------------------------------------------------------------
// createCollection
// ---------------------------------------------------------------------------

describe("createCollection", () => {
	it("returns error when schema.fields is missing instead of throwing", async () => {
		const res = await createCollection(e, {
			collectionName: "bad_request",
		} as never);
		const json = await res.json<{ code: number; message: string }>();

		expect(json.code).toBe(1);
		expect(json.message).toBe(
			"Invalid create collection request: schema.fields must be an array",
		);
	});

	it("rejects non-default dbName for collection operations", async () => {
		const createRes = await createCollection(e, {
			collectionName: "db_guard",
			dbName: "tenant",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION),
		} as never);
		const createJson = await createRes.json<{
			code: number;
			message: string;
		}>();
		expect(createJson.code).toBe(1);
		expect(createJson.message).toMatch(/Unsupported dbName/i);

		const dropRes = await dropCollection(e, {
			collectionName: "db_guard",
			dbName: "tenant",
		} as never);
		const dropJson = await dropRes.json<{ code: number; message: string }>();
		expect(dropJson.code).toBe(1);
		expect(dropJson.message).toMatch(/Unsupported dbName/i);

		const loadRes = await getLoadState(e, {
			collectionName: "db_guard",
			dbName: "tenant",
		} as never);
		const loadJson = await loadRes.json<{ code: number; message: string }>();
		expect(loadJson.code).toBe(1);
		expect(loadJson.message).toMatch(/Unsupported dbName/i);
	});

	it("returns error (code 1) when schema has no FloatVector field", async () => {
		const body = {
			collectionName: "test",
			schema: {
				fields: [{ fieldName: "id", dataType: "Int64", isPrimary: true }],
			},
		};

		const res = await createCollection(e, body);
		const json = await res.json<{ code: number; message: string }>();

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/FloatVector/);
	});

	it("returns error (code 1) when FloatVector field has no dim", async () => {
		const body = {
			collectionName: "test",
			schema: {
				fields: [
					{ fieldName: "id", dataType: "Int64", isPrimary: true },
					{
						fieldName: "vector",
						dataType: "FloatVector",
						elementTypeParams: {},
					},
				],
			},
		};

		const res = await createCollection(e, body);
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(1);
	});

	it("inserts a new non-hybrid collection and returns ok", async () => {
		const body = {
			collectionName: "my_collection",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION),
		};

		const res = await createCollection(e, body);
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(0);

		const row = await e.DB.prepare("SELECT * FROM collections WHERE name = ?")
			.bind("my_collection")
			.first<{
				name: string;
				safe_name: string | null;
				dimension: number;
				is_hybrid: number;
			}>();

		expect(row).not.toBeNull();
		expect(row?.safe_name).toBe("my_collection");
		expect(row?.dimension).toBe(CONFIGURED_VECTOR_DIMENSION);
		expect(row?.is_hybrid).toBe(0);
	});

	it("returns error when schema dimension does not match the configured Vectorize dimension", async () => {
		const res = await createCollection(e, {
			collectionName: "bad_dim",
			schema: validSchema(64),
		});
		const json = await res.json<{ code: number; message: string }>();

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/dimension/i);
	});

	it("returns error when an existing collection row has a mismatched stored dimension", async () => {
		await seedCollection("my_collection", 64);

		const res = await createCollection(e, {
			collectionName: "my_collection",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION),
		});
		const json = await res.json<{ code: number; message: string }>();

		expect(json.code).toBe(1);
		expect(json.message).toContain("64");
		expect(json.message).toContain(String(CONFIGURED_VECTOR_DIMENSION));
	});

	it("rejects an existing non-hybrid collection when hybrid mode is requested", async () => {
		await seedCollection("plain_existing", CONFIGURED_VECTOR_DIMENSION, 0);

		const res = await createCollection(e, {
			collectionName: "plain_existing",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION, true),
		});
		const json = await res.json<{ code: number; message: string }>();

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/mode/i);
	});

	it("rejects an existing hybrid collection when non-hybrid mode is requested", async () => {
		await seedCollection("hybrid_existing", CONFIGURED_VECTOR_DIMENSION, 1);

		const res = await createCollection(e, {
			collectionName: "hybrid_existing",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION),
		});
		const json = await res.json<{ code: number; message: string }>();

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/mode/i);
	});

	it("is idempotent when the collection already exists", async () => {
		const body = {
			collectionName: "my_collection",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION),
		};

		await createCollection(e, body);
		const res = await createCollection(e, body); // second call
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(0);

		// Must not have inserted a duplicate row
		const { results } = await e.DB.prepare(
			"SELECT * FROM collections WHERE name = ?",
		)
			.bind("my_collection")
			.run();

		expect(results).toHaveLength(1);
	});

	it("is idempotent for hybrid collections and ensures FTS table exists on retry", async () => {
		const body = {
			collectionName: "hybrid_idem",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION, true),
		};

		await createCollection(e, body);

		// Drop FTS table to simulate partial creation
		await e.DB.exec("DROP TABLE IF EXISTS fts_hybrid_idem");

		const res = await createCollection(e, body); // retry repairs FTS
		const json = await res.json<{ code: number }>();
		expect(json.code).toBe(0);

		// FTS table must exist after retry
		const fts = await e.DB.prepare(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'fts_hybrid_idem'`,
		).first();
		expect(fts).not.toBeNull();
	});

	it("creates FTS virtual table and sets is_hybrid=1 for a hybrid collection", async () => {
		const body = {
			collectionName: "hybrid_col",
			schema: validSchema(CONFIGURED_VECTOR_DIMENSION, true),
		};

		const res = await createCollection(e, body);
		const json = await res.json<{ code: number }>();

		expect(json.code).toBe(0);

		const row = await e.DB.prepare(
			"SELECT is_hybrid FROM collections WHERE name = ?",
		)
			.bind("hybrid_col")
			.first<{ is_hybrid: number }>();

		expect(row?.is_hybrid).toBe(1);

		const ftsTable = await e.DB.prepare(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
		)
			.bind("fts_hybrid_col")

```

### 7. test/search.spec.ts (lines 201-479)
```ts
describe("searchEntities", () => {
	beforeEach(async () => {
		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collections (name TEXT PRIMARY KEY, safe_name TEXT UNIQUE, dimension INTEGER NOT NULL, is_hybrid INTEGER NOT NULL DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()))`,
		);
		await e.DB.exec(
			`CREATE TABLE IF NOT EXISTS collection_vectors (id TEXT NOT NULL, physical_id TEXT NOT NULL UNIQUE, collection_name TEXT NOT NULL, PRIMARY KEY (id, collection_name))`,
		);
		await e.DB.exec(`DELETE FROM collections`);
		await e.DB.exec(`DELETE FROM collection_vectors`);
		await seedDefaultTrackedIds();
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("returns error code 1 when data array is empty (no query vector)", async () => {
		const body = {
			collectionName: "test_col",
			data: [],
			annsField: "vector",
			limit: 10,
		};

		const res = await searchEntities(e, body as MilvusSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/Missing query vector/i);
	});

	it("returns error when the query vector length does not match the configured dimension", async () => {
		const body = {
			collectionName: "test_col",
			data: [[0.1, 0.2]],
			annsField: "vector",
			limit: 10,
		};

		const res = await searchEntities(e, body as MilvusSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toMatch(/dimension/i);
	});

	it("returns a deterministic error when embedding lookup fails with an empty response", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query");
		vi.spyOn(e.AI, "run").mockResolvedValue({
			data: [],
			shape: [0, 0],
		} as unknown as RerankRunOutput);

		const res = await searchEntities(e, {
			collectionName: "test_col",
			data: ["hello"],
			annsField: "vector",
			limit: 10,
		} as MilvusSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(1);
		expect(json.message).toBe(
			"Invalid embedding response: missing query vector",
		);
		expect(querySpy).not.toHaveBeenCalled();
	});

	it("rejects non-default dbName for both search entrypoints", async () => {
		const searchRes = await searchEntities(e, {
			collectionName: "test_col",
			dbName: "tenant",
			data: [[0.1, 0.2, 0.3]],
			annsField: "vector",
			limit: 10,
		} as never);
		const searchJson = (await searchRes.json()) as Envelope<SearchResult>;
		expect(searchJson.code).toBe(1);
		expect(searchJson.message).toMatch(/Unsupported dbName/i);

		const hybridRes = await hybridSearch(e, {
			collectionName: "test_col",
			dbName: "tenant",

...11185 bytes truncated...

The tool call succeeded but the output was truncated. Full output saved to: /home/jaryl/.local/share/opencode/tool-output/tool_d052442a40019OBfofcntgcq0T
Use the Task tool to have explore agent process this file with Grep and Read (with offset/limit). Do NOT read the full file yourself - delegate to save context.
```

**Tool: cfmantic-code_search_code**

**Input:**
```json
{
  "path": "/home/jaryl/Projects/cf-workers-milvus",
  "query": "where does Workers AI rerank get the query text",
  "limit": 8
}
```

**Output:**
```
Found 8 results for "where does Workers AI rerank get the query text":

### 1. test/search.spec.ts (lines 2379-2644)
```ts
			score: 1 / 61,
			distance: 1 / 61,
			rrfScore: 1 / 61,
			content: "hello",
		});
		expect(json.data[0]).not.toHaveProperty("relativePath");
	});

	it("calls Workers AI rerank when query text exists", async () => {
		await createFtsTable("test_col");
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			await vectorizeMatchesFor("test_col"),
		);
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
			]),
		);
		const aiSpy = vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 1, score: 0.99 },
				{ id: 0, score: 0.25 },
			]),
		);

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 10 },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 2,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(json.data.map((row) => row.id)).toEqual(["id2", "id1"]);
		expect(aiSpy).toHaveBeenCalledWith(
			"@cf/baai/bge-reranker-base",
			expect.objectContaining({
				query: "hello world",
				contexts: [
					expect.objectContaining({ text: "hello" }),
					expect.objectContaining({ text: "world" }),
				],
			}),
		);

		const rerankInput = aiSpy.mock.calls[0][1] as Record<string, unknown>;
		expect(rerankInput).not.toHaveProperty("top_k");
	});

	it("preserves original rrfScore when Workers AI reranking updates score", async () => {
		await createFtsTable("test_col");
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			await vectorizeMatchesFor("test_col"),
		);
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
			]),
		);
		vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 1, score: 0.99 },
				{ id: 0, score: 0.25 },
			]),
		);

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 10 },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 2,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(json.data).toEqual([
			expect.objectContaining({
				id: "id2",
				score: 0.99,
				distance: 0.99,
				rrfScore: 1 / 62,
			}),
			expect.objectContaining({
				id: "id1",
				score: 0.25,
				distance: 0.25,
				rrfScore: 1 / 61,
			}),
		]);
	});

	it("widens the dense candidate pool for Workers AI rerank when dense limit is absent", async () => {
		await createFtsTable("test_col");
		const querySpy = vi
			.spyOn(e.VECTORIZE, "query")
			.mockResolvedValue(await vectorizeMatchesFor("test_col"));
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
			]),
		);
		vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 1, score: 0.99 },
				{ id: 0, score: 0.25 },
			]),
		);

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector" },
				{ data: ["hello world"], annsField: "sparse_vector" },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 5,
		} as unknown as MilvusHybridSearchRequest;

		await hybridSearch(e, body);

		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({ topK: 25, returnMetadata: "none" }),
		);
	});

	it("clamps the widened dense candidate pool to the Vectorize cap for Workers AI rerank", async () => {
		await createFtsTable("test_col");
		const querySpy = vi
			.spyOn(e.VECTORIZE, "query")
			.mockResolvedValue(await vectorizeMatchesFor("test_col"));
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
			]),
		);
		vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 1, score: 0.99 },
				{ id: 0, score: 0.25 },
			]),
		);

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector" },
				{ data: ["hello world"], annsField: "sparse_vector" },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 30,
		} as unknown as MilvusHybridSearchRequest;

		await hybridSearch(e, body);

		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({ topK: 100, returnMetadata: "none" }),
		);
	});

	it("keeps an explicit dense limit for Workers AI rerank", async () => {
		await createFtsTable("test_col");
		const querySpy = vi
			.spyOn(e.VECTORIZE, "query")
			.mockResolvedValue(await vectorizeMatchesFor("test_col"));
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
			]),
		);
		vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 1, score: 0.99 },
				{ id: 0, score: 0.25 },
			]),
		);

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 7 },
				{ data: ["hello world"], annsField: "sparse_vector" },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 30,
		} as unknown as MilvusHybridSearchRequest;

		await hybridSearch(e, body);

		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({ topK: 7, returnMetadata: "none" }),
		);
	});

	it("uses the cheap path when Workers AI rerank has no usable query", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			vectorizeMatches([
				vectorizeMatch({
					id: await physicalId("test_col", "id1"),
					score: 0.95,
				}),
				vectorizeMatch({
					id: await physicalId("test_col", "id2"),
					score: 0.8,
				}),
			]),
		);
		const getByIdsSpy = vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue([
			vectorizeVector({
				id: await physicalId("test_col", "id1"),
				metadata: MOCK_MATCHES[0].metadata,
			}),
		]);
		const aiSpy = vi.spyOn(e.AI, "run");

		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 25 }],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 1,
		} as unknown as MilvusHybridSearchRequest;

		const res = await hybridSearch(e, body);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({ topK: 25, returnMetadata: "none" }),
		);
		expect(getByIdsSpy).toHaveBeenCalledWith([
			await physicalId("test_col", "id1"),
		]);
		expect(aiSpy).not.toHaveBeenCalled();
		expect(json.code).toBe(0);
		expect(json.data.map((row) => row.id)).toEqual(["id1"]);
	});

	it("widens the sparse candidate pool for Workers AI rerank when sparse limit is absent", async () => {
		await createFtsTable("hybrid_col");
		for (let i = 0; i < 25; i++) {
			await e.DB.exec(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('fts${i}', 'hello world content ${i}', 'file${i}.ts', 1, 5, '.ts', '{}')`,
			);
		}

		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches([], 0));
		const aiSpy = vi
			.spyOn(e.AI, "run")

```

### 2. src/rerank.ts (lines 1-107)
Symbol: applyWorkersAIRerank (function, lines 39-69)
```ts
import { RankedResult } from "./rrf";
import { Env, MilvusSearchSubRequest } from "./types";

const DEFAULT_WORKERS_AI_RERANK_MODEL = "@cf/baai/bge-reranker-base";

interface WorkersAiRerankResponse {
	response?: Array<{ id?: number; score?: number }>;
}

type WorkersAiRerankFn = (
	model: string,
	input: { query: string; contexts: Array<{ text: string }> },
) => Promise<WorkersAiRerankResponse>;

function normalizeQueryText(value: unknown): string | null {
	if (typeof value !== "string") return null;
	const trimmed = value.trim();
	return trimmed || null;
}

function extractRequestQuery(
	req: MilvusSearchSubRequest | undefined,
): string | null {
	if (!req) return null;
	const [query] = req.data;
	return normalizeQueryText(query);
}

function buildContextText(data: Record<string, unknown>): string {
	const content = normalizeQueryText(data.content);
	if (content) return content;

	const relativePath = normalizeQueryText(data.relativePath);
	if (relativePath) return relativePath;

	return JSON.stringify(data);
}

function applyWorkersAIRerank<T extends RankedResult>(
	results: T[],
	response: WorkersAiRerankResponse,
): T[] {
	const scoresByIndex = new Map<number, number>();

	for (const item of response.response ?? []) {
		if (typeof item.id !== "number" || typeof item.score !== "number") continue;
		if (item.id < 0 || item.id >= results.length) continue;
		scoresByIndex.set(item.id, item.score);
	}

	if (scoresByIndex.size === 0) return results;

	return results
		.map((result, index) => ({
			index,
			rerankScore: scoresByIndex.get(index),
			result,
		}))
		.sort((a, b) => {
			if (a.rerankScore === undefined && b.rerankScore === undefined)
				return a.index - b.index;
			if (a.rerankScore === undefined) return 1;
			if (b.rerankScore === undefined) return -1;
			return b.rerankScore - a.rerankScore || a.index - b.index;
		})
		.map(({ rerankScore, result }) =>
			rerankScore === undefined ? result : { ...result, score: rerankScore },
		);
}

export function resolveHybridRerankQuery(
	denseReq: MilvusSearchSubRequest,
	sparseReq: MilvusSearchSubRequest | undefined,
	params: Record<string, unknown> | undefined,
): string | null {
	return (
		normalizeQueryText(params?.query) ??
		extractRequestQuery(sparseReq) ??
		extractRequestQuery(denseReq)
	);
}

export async function rerankWithWorkersAI<T extends RankedResult>(
	env: Env,
	results: T[],
	query: string | null,
): Promise<T[]> {
	const normalizedQuery = normalizeQueryText(query);
	if (!normalizedQuery || results.length < 2) return results;

	try {
		const response = await (env.AI.run as WorkersAiRerankFn)(
			DEFAULT_WORKERS_AI_RERANK_MODEL,
			{
				query: normalizedQuery,
				contexts: results.map((result) => ({
					text: buildContextText(result.data),
				})),
			},
		);

		return applyWorkersAIRerank(results, response);
	} catch {
		return results;
	}
}

```

### 3. test/rerank.spec.ts (lines 1-256)
```ts
import { describe, expect, it, vi } from "vitest";
import { RankedResult } from "../src/rrf";
import { rerankWithWorkersAI, resolveHybridRerankQuery } from "../src/rerank";
import type { Env, MilvusSearchSubRequest } from "../src/types";

type RerankRunOutput = Awaited<ReturnType<Env["AI"]["run"]>>;

function makeDenseStringRequest(query: string): MilvusSearchSubRequest {
	return {
		annsField: "vector",
		data: [query],
		limit: 10,
	};
}

function makeDenseVectorRequest(): MilvusSearchSubRequest {
	return {
		annsField: "vector",
		data: [[0.1, 0.2, 0.3]],
		limit: 10,
	};
}

function makeSparseRequest(query: string): MilvusSearchSubRequest {
	return {
		annsField: "sparse_vector",
		data: [query],
		limit: 10,
	};
}

function makeResult(
	id: string,
	score: number,
	data: Record<string, unknown> = {},
): RankedResult {
	return { id, score, data };
}

function makeEnv(run = vi.fn()): Env {
	return {
		AI: { run },
	} as unknown as Env;
}

function makeRerankResponse(
	response: Array<{ id?: number; score?: number }>,
): RerankRunOutput {
	return { response } as unknown as RerankRunOutput;
}

describe("resolveHybridRerankQuery", () => {
	it("prefers params query over sparse and dense queries", () => {
		const query = resolveHybridRerankQuery(
			makeDenseStringRequest("dense query"),
			makeSparseRequest("sparse query"),
			{ query: "params query" },
		);

		expect(query).toBe("params query");
	});

	it("falls back to sparse query when params query is missing", () => {
		const query = resolveHybridRerankQuery(
			makeDenseStringRequest("dense query"),
			makeSparseRequest("sparse query"),
			undefined,
		);

		expect(query).toBe("sparse query");
	});

	it("falls back to dense string query when sparse query is missing", () => {
		const query = resolveHybridRerankQuery(
			makeDenseStringRequest("dense query"),
			undefined,
			undefined,
		);

		expect(query).toBe("dense query");
	});

	it("returns null when no usable rerank query exists", () => {
		const query = resolveHybridRerankQuery(
			makeDenseVectorRequest(),
			undefined,
			{ query: "   " },
		);

		expect(query).toBeNull();
	});
});

describe("rerankWithWorkersAI", () => {
	it("skips AI.run when query is null", async () => {
		const run = vi.fn();
		const testEnv = makeEnv(run);
		const results = [makeResult("a", 0.7), makeResult("b", 0.6)];

		const reranked = await rerankWithWorkersAI(testEnv, results, null);

		expect(run).not.toHaveBeenCalled();
		expect(reranked).toEqual(results);
	});

	it("skips AI.run when query is blank", async () => {
		const run = vi.fn();
		const testEnv = makeEnv(run);
		const results = [makeResult("a", 0.7), makeResult("b", 0.6)];

		const reranked = await rerankWithWorkersAI(testEnv, results, "   ");

		expect(run).not.toHaveBeenCalled();
		expect(reranked).toEqual(results);
	});

	it("skips AI.run when fewer than two results are present", async () => {
		const run = vi.fn();
		const testEnv = makeEnv(run);
		const results = [makeResult("a", 0.7)];

		const reranked = await rerankWithWorkersAI(testEnv, results, "hello");

		expect(run).not.toHaveBeenCalled();
		expect(reranked).toEqual(results);
	});

	it("uses content then relativePath then JSON data for rerank contexts", async () => {
		const run = vi
			.fn()
			.mockResolvedValue(makeRerankResponse([{ id: 0, score: 0.9 }]));
		const testEnv = makeEnv(run);
		const results = [
			makeResult("a", 0.7, { content: "content text", relativePath: "a.ts" }),
			makeResult("b", 0.6, { content: "   ", relativePath: "b.ts" }),
			makeResult("c", 0.5, { fileExtension: ".ts", startLine: 3 }),
		];

		await rerankWithWorkersAI(testEnv, results, "hello");

		expect(run).toHaveBeenCalledWith("@cf/baai/bge-reranker-base", {
			query: "hello",
			contexts: [
				{ text: "content text" },
				{ text: "b.ts" },
				{ text: JSON.stringify({ fileExtension: ".ts", startLine: 3 }) },
			],
		});
	});

	it("applies rerank ordering and scores from valid responses", async () => {
		const testEnv = makeEnv(
			vi.fn().mockResolvedValue(
				makeRerankResponse([
					{ id: 2, score: 0.99 },
					{ id: 0, score: 0.75 },
					{ id: 1, score: 0.25 },
				]),
			),
		);
		const results = [
			makeResult("a", 0.7, { content: "alpha" }),
			makeResult("b", 0.6, { content: "beta" }),
			makeResult("c", 0.5, { content: "gamma" }),
		];

		const reranked = await rerankWithWorkersAI(testEnv, results, "hello");

		expect(reranked).toEqual([
			expect.objectContaining({ id: "c", score: 0.99 }),
			expect.objectContaining({ id: "a", score: 0.75 }),
			expect.objectContaining({ id: "b", score: 0.25 }),
		]);
	});

	it("ignores invalid rerank response entries without breaking original order", async () => {
		const testEnv = makeEnv(
			vi
				.fn()
				.mockResolvedValue(
					makeRerankResponse([
						{ id: 1, score: 0.91 },
						{ id: -1, score: 0.5 },
						{ id: 8, score: 0.4 },
						{ id: 0 },
						{ score: 0.3 },
					]),
				),
		);
		const results = [
			makeResult("a", 0.7, { content: "alpha" }),
			makeResult("b", 0.6, { content: "beta" }),
			makeResult("c", 0.5, { content: "gamma" }),
		];

		const reranked = await rerankWithWorkersAI(testEnv, results, "hello");

		expect(reranked).toEqual([
			expect.objectContaining({ id: "b", score: 0.91 }),
			expect.objectContaining({ id: "a", score: 0.7 }),
			expect.objectContaining({ id: "c", score: 0.5 }),
		]);
	});

	it("returns original results when the rerank response is missing scores", async () => {
		const testEnv = makeEnv(vi.fn().mockResolvedValue({} as RerankRunOutput));
		const results = [
			makeResult("a", 0.7, { content: "alpha" }),
			makeResult("b", 0.6, { content: "beta" }),
		];

		const reranked = await rerankWithWorkersAI(testEnv, results, "hello");

		expect(reranked).toEqual(results);
	});

	it("keeps original order when rerank scores are tied", async () => {
		const testEnv = makeEnv(
			vi.fn().mockResolvedValue(
				makeRerankResponse([
					{ id: 1, score: 0.91 },
					{ id: 0, score: 0.91 },
					{ id: 2, score: 0.2 },
				]),
			),
		);
		const results = [
			makeResult("a", 0.7, { content: "alpha" }),
			makeResult("b", 0.6, { content: "beta" }),
			makeResult("c", 0.5, { content: "gamma" }),
		];

		const reranked = await rerankWithWorkersAI(testEnv, results, "hello");

		expect(reranked).toEqual([
			expect.objectContaining({ id: "a", score: 0.91 }),
			expect.objectContaining({ id: "b", score: 0.91 }),
			expect.objectContaining({ id: "c", score: 0.2 }),
		]);
	});

	it("falls back to original order when AI.run fails", async () => {
		const testEnv = makeEnv(
			vi.fn().mockRejectedValue(new Error("rerank failed")),
		);
		const results = [
			makeResult("a", 0.7, { content: "alpha" }),
			makeResult("b", 0.6, { content: "beta" }),
		];

		const reranked = await rerankWithWorkersAI(testEnv, results, "hello");

		expect(reranked).toEqual(results);
	});
});

```

### 4. test/search.spec.ts (lines 2635-2910)
```ts
		await createFtsTable("hybrid_col");
		for (let i = 0; i < 25; i++) {
			await e.DB.exec(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('fts${i}', 'hello world content ${i}', 'file${i}.ts', 1, 5, '.ts', '{}')`,
			);
		}

		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches([], 0));
		const aiSpy = vi
			.spyOn(e.AI, "run")
			.mockResolvedValue(makeRerankResponse([{ id: 19, score: 0.99 }]));

		const body = {
			collectionName: "hybrid_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 1 },
				{ data: ["hello world"], annsField: "sparse_vector" },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 5,
		} as unknown as MilvusHybridSearchRequest;

		await hybridSearch(e, body);

		const rerankInput = aiSpy.mock.calls[0][1] as {
			contexts: Array<{ text: string }>;
		};
		expect(rerankInput.contexts).toHaveLength(20);
	});

	it("caps the post-fusion Workers AI rerank pool before hydration and reranking", async () => {
		await createFtsTable("hybrid_col");
		await e.DB.exec(
			`INSERT INTO collections (name, dimension, is_hybrid) VALUES ('hybrid_col', 3, 1)`,
		);

		for (let i = 0; i < 25; i++) {
			await e.DB.exec(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('sparse${i}', 'hello world content ${i}', 'file${i}.ts', 1, 5, '.ts', '{}')`,
			);
		}

		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			vectorizeMatches(
				await Promise.all(
					Array.from({ length: 25 }, async (_, i) =>
						vectorizeMatch({
							id: await physicalId("hybrid_col", `dense${i}`),
							score: 1 - i / 100,
						}),
					),
				),
			),
		);
		const getByIdsSpy = vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await Promise.all(
				Array.from({ length: 25 }, async (_, i) =>
					vectorizeVector({
						id: await physicalId("hybrid_col", `dense${i}`),
						metadata: {
							content: `dense content ${i}`,
							relativePath: `dense${i}.ts`,
							startLine: 1,
							endLine: 5,
							fileExtension: ".ts",
							metadata: "{}",
						},
					}),
				),
			),
		);
		const aiSpy = vi
			.spyOn(e.AI, "run")
			.mockResolvedValue(makeRerankResponse([{ id: 19, score: 0.99 }]));

		await hybridSearch(e, {
			collectionName: "hybrid_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 25 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 25 },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 2,
		} as MilvusHybridSearchRequest);

		const hydratedIds = getByIdsSpy.mock.calls[0]?.[0] as string[];
		const rerankInput = aiSpy.mock.calls[0]?.[1] as {
			contexts: Array<{ text: string }>;
		};

		expect(hydratedIds.length).toBeLessThanOrEqual(20);
		expect(hydratedIds.every((id) => /^[0-9a-f]{64}$/.test(id))).toBe(true);
		expect(rerankInput.contexts).toHaveLength(20);
	});

	it("keeps an explicit sparse limit for Workers AI rerank", async () => {
		await createFtsTable("hybrid_col");
		for (let i = 0; i < 10; i++) {
			await e.DB.exec(
				`INSERT INTO fts_hybrid_col (id, content, relativePath, startLine, endLine, fileExtension, metadata) VALUES ('fts${i}', 'hello world content ${i}', 'file${i}.ts', 1, 5, '.ts', '{}')`,
			);
		}

		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(vectorizeMatches([], 0));
		const aiSpy = vi
			.spyOn(e.AI, "run")
			.mockResolvedValue(makeRerankResponse([{ id: 2, score: 0.99 }]));

		const body = {
			collectionName: "hybrid_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 1 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 3 },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 30,
		} as unknown as MilvusHybridSearchRequest;

		await hybridSearch(e, body);

		const rerankInput = aiSpy.mock.calls[0][1] as {
			contexts: Array<{ text: string }>;
		};
		expect(rerankInput.contexts).toHaveLength(3);
	});

	it("applies final slicing after Workers AI reranking", async () => {
		await createFtsTable("test_col");
		const denseMatches = [
			vectorizeMatch({
				id: "id1",
				score: 0.95,
				metadata: MOCK_MATCHES[0].metadata,
			}),
			vectorizeMatch({
				id: "id2",
				score: 0.8,
				metadata: MOCK_MATCHES[1].metadata,
			}),
			vectorizeMatch({
				id: "id3",
				score: 0.7,
				metadata: {
					content: "third",
					relativePath: "c.ts",
					startLine: 1,
					endLine: 2,
					fileExtension: ".ts",
					metadata: "{}",
				},
			}),
		];
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			await vectorizeMatchesFor("test_col", denseMatches),
		);
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
				{
					id: "id3",
					metadata: {
						content: "third",
						relativePath: "c.ts",
						startLine: 1,
						endLine: 2,
						fileExtension: ".ts",
						metadata: "{}",
					},
				},
			]),
		);
		const aiSpy = vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 2, score: 0.99 },
				{ id: 0, score: 0.5 },
				{ id: 1, score: 0.25 },
			]),
		);

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 3 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 3 },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 1,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(json.data).toHaveLength(1);
		expect(json.data[0].id).toBe("id3");

		const rerankInput = aiSpy.mock.calls[0][1] as {
			contexts: Array<{ text: string }>;
		};
		expect(rerankInput.contexts).toHaveLength(3);
	});

	it("hydrates dense ids-only results before Workers AI reranking", async () => {
		const querySpy = vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			vectorizeMatches([
				vectorizeMatch({
					id: await physicalId("test_col", "id1"),
					score: 0.95,
				}),
				vectorizeMatch({
					id: await physicalId("test_col", "id2"),
					score: 0.8,
				}),
			]),
		);
		const getByIdsSpy = vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue([
			vectorizeVector({
				id: await physicalId("test_col", "id1"),
				metadata: MOCK_MATCHES[0].metadata,
			}),
			vectorizeVector({
				id: await physicalId("test_col", "id2"),
				metadata: MOCK_MATCHES[1].metadata,
			}),
		]);
		const aiSpy = vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 1, score: 0.99 },
				{ id: 0, score: 0.25 },
			]),
		);

		const body = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 25 }],
			rerank: {
				strategy: "workers_ai",
				params: { k: 60, query: "hello world" },
			},
			limit: 1,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(json.data[0].id).toBe("id2");
		expect(querySpy).toHaveBeenCalledWith(
			[0.1, 0.2, 0.3],
			expect.objectContaining({ topK: 25, returnMetadata: "none" }),
		);
		expect(getByIdsSpy).toHaveBeenCalledWith([
			await physicalId("test_col", "id1"),
			await physicalId("test_col", "id2"),
		]);

		const rerankInput = aiSpy.mock.calls[0][1] as {
			contexts: Array<{ text: string }>;
		};
		expect(rerankInput.contexts).toEqual([
			{ text: "hello" },
			{ text: "world" },
		]);
	});

	it("batches ids-only hydration for Workers AI rerank above 20 ids", async () => {
		const denseMatches = await Promise.all(
			Array.from({ length: 36 }, async (_, i) =>
				vectorizeMatch({
					id: await physicalId("test_col", `id${i}`),
					score: 1 - i / 100,
				}),
			),
		);
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(

```

### 5. test/search.spec.ts (lines 2895-3090)
```ts
		expect(rerankInput.contexts).toEqual([
			{ text: "hello" },
			{ text: "world" },
		]);
	});

	it("batches ids-only hydration for Workers AI rerank above 20 ids", async () => {
		const denseMatches = await Promise.all(
			Array.from({ length: 36 }, async (_, i) =>
				vectorizeMatch({
					id: await physicalId("test_col", `id${i}`),
					score: 1 - i / 100,
				}),
			),
		);
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			vectorizeMatches(denseMatches),
		);
		const getByIdsSpy = vi
			.spyOn(e.VECTORIZE, "getByIds")
			.mockImplementation(async (ids: string[]) => {
				if (ids.length > VECTORIZE_METADATA_TOPK_CAP) {
					throw new Error(
						`too many ids in payload; max id count is ${VECTORIZE_METADATA_TOPK_CAP}, got ${ids.length}`,
					);
				}

				return ids.map((id) =>
					vectorizeVector({
						id,
						metadata: {
							content: `content ${id}`,
							relativePath: `${id}.ts`,
							startLine: 1,
							endLine: 5,
							fileExtension: ".ts",
							metadata: "{}",
						},
					}),
				);
			});
		const aiSpy = vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 35, score: 0.99 },
				{ id: 0, score: 0.5 },
			]),
		);

		const res = await hybridSearch(e, {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 36 }],
			rerank: {
				strategy: "workers_ai",
				params: { k: 60, query: "hello world" },
			},
			limit: 8,
		} as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		const hydratedIds = getByIdsSpy.mock.calls.flatMap(
			([ids]) => ids as string[],
		);
		const rerankInput = aiSpy.mock.calls[0]?.[1] as {
			contexts: Array<{ text: string }>;
		};

		expect(json.code).toBe(0);
		expect(json.data).toHaveLength(8);
		expect(json.data.slice(0, 2).map((row) => row.id)).toEqual(["id35", "id0"]);
		expect(getByIdsSpy).toHaveBeenCalledTimes(2);
		expect(
			getByIdsSpy.mock.calls.every(
				([ids]) => (ids as string[]).length <= VECTORIZE_METADATA_TOPK_CAP,
			),
		).toBe(true);
		expect(hydratedIds).toEqual(denseMatches.map((match) => match.id));
		expect(rerankInput.contexts).toHaveLength(36);
	});

	it("skips Workers AI reranking for vector-only search unless params.query is provided", async () => {
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			await vectorizeMatchesFor("test_col"),
		);
		const aiSpy = vi.spyOn(e.AI, "run").mockResolvedValue(
			makeRerankResponse([
				{ id: 1, score: 0.99 },
				{ id: 0, score: 0.25 },
			]),
		);

		const withoutQuery = {
			collectionName: "test_col",
			search: [{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 }],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 2,
		};

		const noQueryRes = await hybridSearch(
			e,
			withoutQuery as MilvusHybridSearchRequest,
		);
		const noQueryJson = (await noQueryRes.json()) as Envelope<SearchResult>;

		expect(noQueryJson.code).toBe(0);
		expect(noQueryJson.data.map((row) => row.id)).toEqual(["id1", "id2"]);
		expect(aiSpy).not.toHaveBeenCalled();

		const withQueryRes = await hybridSearch(e, {
			...withoutQuery,
			rerank: {
				strategy: "workers_ai",
				params: { k: 60, query: "hello world" },
			},
		} as MilvusHybridSearchRequest);
		const withQueryJson = (await withQueryRes.json()) as Envelope<SearchResult>;

		expect(withQueryJson.code).toBe(0);
		expect(withQueryJson.data.map((row) => row.id)).toEqual(["id2", "id1"]);
		expect(aiSpy).toHaveBeenCalledOnce();
	});

	it("falls back to RRF order when Workers AI reranking fails", async () => {
		await createFtsTable("test_col");
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			await vectorizeMatchesFor("test_col"),
		);
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
			]),
		);
		vi.spyOn(e.AI, "run").mockRejectedValue(new Error("rerank failed"));

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 10 },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 2,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(json.data.map((row) => row.id)).toEqual(["id1", "id2"]);
		expect(e.AI.run).toHaveBeenCalledOnce();
	});

	it("preserves rrfScore when Workers AI reranking falls back to RRF order", async () => {
		await createFtsTable("test_col");
		vi.spyOn(e.VECTORIZE, "query").mockResolvedValue(
			await vectorizeMatchesFor("test_col"),
		);
		vi.spyOn(e.VECTORIZE, "getByIds").mockResolvedValue(
			await vectorizeVectorsFor("test_col", [
				{ id: "id1", metadata: MOCK_MATCHES[0].metadata },
				{ id: "id2", metadata: MOCK_MATCHES[1].metadata },
			]),
		);
		vi.spyOn(e.AI, "run").mockRejectedValue(new Error("rerank failed"));

		const body = {
			collectionName: "test_col",
			search: [
				{ data: [[0.1, 0.2, 0.3]], annsField: "vector", limit: 10 },
				{ data: ["hello world"], annsField: "sparse_vector", limit: 10 },
			],
			rerank: { strategy: "workers_ai", params: { k: 60 } },
			limit: 2,
		};

		const res = await hybridSearch(e, body as MilvusHybridSearchRequest);
		const json = (await res.json()) as Envelope<SearchResult>;

		expect(json.code).toBe(0);
		expect(json.data).toEqual([
			expect.objectContaining({
				id: "id1",
				score: 1 / 61,
				distance: 1 / 61,
				rrfScore: 1 / 61,
			}),
			expect.objectContaining({
				id: "id2",
				score: 1 / 62,
				distance: 1 / 62,
				rrfScore: 1 / 62,
			}),
		]);
	});
});

```

### 6. src/search.ts (lines 1-301)
```ts
import {
	Env,
	EntityData,
	EntityRow,
	MilvusSearchRequest,
	MilvusHybridSearchRequest,
	getConfiguredVectorizeDimension,
	ok,
	err,
	validateDefaultDbName,
	toSafeName,
	toPhysicalVectorizeIds,
	ENTITY_FIELDS,
	VectorizeEntityMetadata,
} from "./types";
import { rrfFuse, RankedResult } from "./rrf";
import { InvalidEmbeddingResponseError, embedQuery } from "./embedding";
import { filterFields } from "./entities";
import {
	compileFilterPlanToSql,
	compileFilterPlanToVectorize,
	isUnsupportedFilterError,
	parseFilterPlan,
} from "./filters";
import { rerankWithWorkersAI, resolveHybridRerankQuery } from "./rerank";

export const VECTORIZE_METADATA_TOPK_CAP = 20;
export const VECTORIZE_IDS_ONLY_TOPK_CAP = 100;

const DEFAULT_VECTORIZE_TOPK = 10;
const HYBRID_RERANK_POOL_MULTIPLIER = 5;
const HYBRID_RERANK_POOL_MIN = 20;
const HYBRID_RERANK_POOL_MAX = 100;
const SPARSE_RERANK_POOL_MULTIPLIER = 3;
const SPARSE_RERANK_POOL_MIN = 20;
const SPARSE_RERANK_POOL_MAX = 50;
const D1_MAX_BOUND_PARAMS = 100;

type CollectionVectorMapping = {
	id: string;
	physical_id: string;
};

function parseOptionalFilterPlan(
	filter: string | undefined,
):
	| { ok: true; plan: ReturnType<typeof parseFilterPlan> | null }
	| { ok: false; response: Response } {
	if (!filter?.trim()) return { ok: true, plan: null };

	try {
		return { ok: true, plan: parseFilterPlan(filter) };
	} catch (error) {
		if (isUnsupportedFilterError(error)) {
			return { ok: false, response: err(1, error.message) };
		}
		throw error;
	}
}

function extractEntityFields(
	meta: VectorizeEntityMetadata | undefined,
): EntityData {
	return {
		content: meta?.content ?? "",
		relativePath: meta?.relativePath ?? "",
		startLine: meta?.startLine ?? 0,
		endLine: meta?.endLine ?? 0,
		fileExtension: meta?.fileExtension ?? "",
		metadata: meta?.metadata ?? "{}",
	};
}

function validateVectorDimension(env: Env, vector: number[]): Response | null {
	const configuredDimension = getConfiguredVectorizeDimension(env);
	if (vector.length === configuredDimension) return null;
	return err(
		1,
		`Vector dimension ${vector.length} does not match configured Vectorize dimension ${configuredDimension}`,
	);
}

function mapEmbeddingResponseError(error: unknown): Response | null {
	if (error instanceof InvalidEmbeddingResponseError) {
		return err(1, error.message);
	}

	return null;
}

function validateOptionalPositiveInteger(
	value: unknown,
	label: string,
): Response | null {
	if (value === undefined) return null;
	if (
		typeof value === "number" &&
		Number.isFinite(value) &&
		Number.isInteger(value) &&
		value > 0
	) {
		return null;
	}

	return err(1, `Invalid ${label}: must be a finite positive integer`);
}

function clampVectorizeTopK(limit: number | undefined): number {
	return Math.min(limit ?? DEFAULT_VECTORIZE_TOPK, VECTORIZE_IDS_ONLY_TOPK_CAP);
}

function resolveLimit(limit: number | undefined): number {
	return limit ?? DEFAULT_VECTORIZE_TOPK;
}

function resolveHybridRerankPoolLimit(finalLimit: number): number {
	return Math.min(
		Math.max(
			finalLimit * HYBRID_RERANK_POOL_MULTIPLIER,
			HYBRID_RERANK_POOL_MIN,
		),
		HYBRID_RERANK_POOL_MAX,
	);
}

function resolveSparseRerankPoolLimit(finalLimit: number): number {
	return Math.min(
		Math.max(
			finalLimit * SPARSE_RERANK_POOL_MULTIPLIER,
			SPARSE_RERANK_POOL_MIN,
		),
		SPARSE_RERANK_POOL_MAX,
	);
}

function getVectorizeReturnMetadata(
	topK: number,
): VectorizeMetadataRetrievalLevel {
	return topK <= VECTORIZE_METADATA_TOPK_CAP ? "all" : "none";
}

function extractEntityFieldsFromRecord(
	data: Record<string, unknown> | undefined,
): EntityData {
	return extractEntityFields(data as VectorizeEntityMetadata | undefined);
}

function hasEntityFields(data: Record<string, unknown>): boolean {
	return ENTITY_FIELDS.every((field) => Object.hasOwn(data, field));
}

async function hydrateEntityFieldsById(
	env: Env,
	collectionName: string,
	safeName: string,
	ids: string[],
): Promise<Map<string, EntityData>> {
	const uniqueIds = Array.from(new Set(ids));
	const hydrated = new Map<string, EntityData>();

	for (
		let offset = 0;
		offset < uniqueIds.length;
		offset += VECTORIZE_METADATA_TOPK_CAP
	) {
		const logicalIds = uniqueIds.slice(
			offset,
			offset + VECTORIZE_METADATA_TOPK_CAP,
		);
		const physicalIds = await toPhysicalVectorizeIds(
			collectionName,
			logicalIds,
		);
		const logicalIdByPhysicalId = new Map(
			physicalIds.map((physicalId, index) => [physicalId, logicalIds[index]]),
		);
		const vectors = await env.VECTORIZE.getByIds(physicalIds);

		for (const vector of vectors) {
			if (vector.namespace && vector.namespace !== safeName) continue;
			const logicalId = logicalIdByPhysicalId.get(vector.id);
			if (!logicalId) continue;
			hydrated.set(
				logicalId,
				extractEntityFields(
					vector.metadata as VectorizeEntityMetadata | undefined,
				),
			);
		}
	}

	return hydrated;
}

async function maskDenseMatchesByLiveIds(
	env: Env,
	collectionName: string,
	matches: VectorizeMatch[],
): Promise<VectorizeMatch[]> {
	if (matches.length === 0) return matches;

	const logicalIdByPhysicalId = new Map<string, string>();
	const uniqueIds = Array.from(new Set(matches.map((match) => match.id)));
	const maxIdsPerStmt = D1_MAX_BOUND_PARAMS - 1;

	for (let offset = 0; offset < uniqueIds.length; offset += maxIdsPerStmt) {
		const ids = uniqueIds.slice(offset, offset + maxIdsPerStmt);
		const placeholders = ids.map(() => "?").join(", ");
		const { results } = await env.DB.prepare(
			`SELECT id, physical_id FROM collection_vectors WHERE collection_name = ? AND physical_id IN (${placeholders})`,
		)
			.bind(collectionName, ...ids)
			.run<CollectionVectorMapping>();

		for (const row of results) {
			logicalIdByPhysicalId.set(row.physical_id, row.id);
		}
	}

	return matches.flatMap((match) => {
		const logicalId = logicalIdByPhysicalId.get(match.id);
		return logicalId ? [{ ...match, id: logicalId }] : [];
	});
}

async function queryDenseRankedResults(
	env: Env,
	queryVector: number[],
	collectionName: string,
	safeName: string,
	topK: number,
	forceIdsOnlyMode: boolean,
	vectorizeFilter: VectorizeVectorMetadataFilter | undefined,
): Promise<{ results: RankedResult[]; usesIdsOnlyMode: boolean }> {
	const returnMetadata = forceIdsOnlyMode
		? "none"
		: getVectorizeReturnMetadata(topK);
	const matches = await env.VECTORIZE.query(queryVector, {
		topK,
		namespace: safeName,
		returnMetadata,
		returnValues: false,
		...(vectorizeFilter && { filter: vectorizeFilter }),
	});
	const liveMatches = await maskDenseMatchesByLiveIds(
		env,
		collectionName,
		matches.matches || [],
	);

	return {
		usesIdsOnlyMode: returnMetadata === "none",
		results: liveMatches.map((match) => ({
			id: match.id,
			score: match.score,
			data:
				returnMetadata === "all"
					? extractEntityFields(
							match.metadata as VectorizeEntityMetadata | undefined,
						)
					: {},
		})),
	};
}

async function hydrateRankedResults<T extends RankedResult>(
	env: Env,
	collectionName: string,
	safeName: string,
	results: T[],
): Promise<T[]> {
	if (results.length === 0) return results;

	const idsToHydrate = Array.from(
		new Set(
			results
				.filter((result) => !hasEntityFields(result.data))
				.map((result) => result.id),
		),
	);

	if (idsToHydrate.length === 0) return results;

	const hydratedById = await hydrateEntityFieldsById(
		env,
		collectionName,
		safeName,
		idsToHydrate,
	);

	return results.map((result) => {
		if (hasEntityFields(result.data)) return result;

		return {
			...result,
			data:
				hydratedById.get(result.id) ??
				extractEntityFieldsFromRecord(result.data),
		} as T;
	});
}


```

### 7. src/search.ts (lines 303-540)
Symbol: hybridSearch (function, lines 413-540)
```ts
async function querySparseRankedResults(
	env: Env,
	safeName: string,
	queryText: string | null,
	topK: number,
	filterPlan: ReturnType<typeof parseFilterPlan> | null,
): Promise<RankedResult[]> {
	if (!queryText) return [];

	const ftsQuery = queryText
		.replaceAll(/[^\w\s]/g, " ")
		.split(/\s+/)
		.filter((w) => w.length > 1)
		.map((w) => `"${w}"`)
		.join(" OR ");

	if (!ftsQuery) return [];

	let ftsSql = `SELECT id, ${ENTITY_FIELDS.join(", ")}, -rank AS score
								FROM fts_${safeName}
								WHERE fts_${safeName} MATCH ?`;
	const bindParams: (string | number)[] = [ftsQuery];

	if (filterPlan?.clauses.length) {
		const compiled = compileFilterPlanToSql(filterPlan);
		ftsSql += ` AND ${compiled.sql}`;
		bindParams.push(...compiled.params);
	}

	ftsSql += " ORDER BY rank LIMIT ?";
	bindParams.push(topK);

	const { results } = await env.DB.prepare(ftsSql)
		.bind(...bindParams)
		.run<EntityRow & { score: number }>();

	return results.map((result) => ({
		id: result.id,
		score: result.score,
		data: {
			content: result.content,
			relativePath: result.relativePath,
			startLine: result.startLine,
			endLine: result.endLine,
			fileExtension: result.fileExtension,
			metadata: result.metadata,
		},
	}));
}

export async function searchEntities(
	env: Env,
	body: MilvusSearchRequest,
): Promise<Response> {
	const dbNameError = validateDefaultDbName(body.dbName);
	if (dbNameError) return dbNameError;

	const { collectionName, data, limit, filter, outputFields } = body;
	const invalidLimit = validateOptionalPositiveInteger(limit, "limit");
	if (invalidLimit) return invalidLimit;

	if (!data[0]) return err(1, "Missing query vector in data[0]");
	const parsedFilter = parseOptionalFilterPlan(filter);
	if (!parsedFilter.ok) return parsedFilter.response;

	let queryVector: number[];
	try {
		queryVector =
			typeof data[0] === "string" ? await embedQuery(env, data[0]) : data[0];
	} catch (error) {
		const embeddingError = mapEmbeddingResponseError(error);
		if (embeddingError) return embeddingError;
		throw error;
	}
	const invalidDimension = validateVectorDimension(env, queryVector);
	if (invalidDimension) return invalidDimension;

	const safeName = toSafeName(collectionName);

	const vectorizeFilter = parsedFilter.plan
		? compileFilterPlanToVectorize(parsedFilter.plan)
		: undefined;
	const topK = clampVectorizeTopK(limit);
	const { results: rankedResults, usesIdsOnlyMode } =
		await queryDenseRankedResults(
			env,
			queryVector,
			collectionName,
			safeName,
			topK,
			false,
			vectorizeFilter,
		);
	const hydratedResults = usesIdsOnlyMode
		? await hydrateRankedResults(env, collectionName, safeName, rankedResults)
		: rankedResults;

	const results = hydratedResults.map((result) => ({
		id: result.id,
		distance: result.score,
		...extractEntityFieldsFromRecord(result.data),
	}));

	const filtered = outputFields?.length
		? filterFields(results, ["id", "distance", ...outputFields])
		: results;

	return ok(filtered);
}

export async function hybridSearch(
	env: Env,
	body: MilvusHybridSearchRequest,
): Promise<Response> {
	const dbNameError = validateDefaultDbName(body.dbName);
	if (dbNameError) return dbNameError;

	const { collectionName, search, rerank, limit, filter, outputFields } = body;
	const invalidLimit = validateOptionalPositiveInteger(limit, "limit");
	if (invalidLimit) return invalidLimit;
	const invalidRerankK = validateOptionalPositiveInteger(
		rerank?.params?.k,
		"rerank.params.k",
	);
	if (invalidRerankK) return invalidRerankK;

	const denseReq = search.find((s) => s.annsField === "vector");
	const sparseReq = search.find((s) => s.annsField === "sparse_vector");

	if (!denseReq)
		return err(1, "Missing dense search request (annsField=vector)");
	const invalidDenseLimit = validateOptionalPositiveInteger(
		denseReq.limit,
		"dense search limit",
	);
	if (invalidDenseLimit) return invalidDenseLimit;
	const invalidSparseLimit = validateOptionalPositiveInteger(
		sparseReq?.limit,
		"sparse search limit",
	);
	if (invalidSparseLimit) return invalidSparseLimit;
	if (!denseReq.data[0]) return err(1, "Missing query vector in data[0]");

	const denseFilter = denseReq.filter || filter;
	const sparseFilter = sparseReq?.filter || filter;
	const parsedDenseFilter = parseOptionalFilterPlan(denseFilter);
	if (!parsedDenseFilter.ok) return parsedDenseFilter.response;
	const parsedSparseFilter = parseOptionalFilterPlan(sparseFilter);
	if (!parsedSparseFilter.ok) return parsedSparseFilter.response;

	let queryVector: number[];
	try {
		queryVector =
			typeof denseReq.data[0] === "string"
				? await embedQuery(env, denseReq.data[0])
				: (denseReq.data as number[][])[0];
	} catch (error) {
		const embeddingError = mapEmbeddingResponseError(error);
		if (embeddingError) return embeddingError;
		throw error;
	}
	const invalidDimension = validateVectorDimension(env, queryVector);
	if (invalidDimension) return invalidDimension;
	const queryText = sparseReq ? (sparseReq.data as string[])[0] : null;
	const finalLimit = resolveLimit(limit);
	const useWorkersAIRerank = rerank?.strategy === "workers_ai";
	const rrfK = (rerank?.params?.k as number | undefined) ?? 60;
	const safeName = toSafeName(collectionName);
	const rerankPoolLimit = resolveHybridRerankPoolLimit(finalLimit);
	const sparsePoolLimit = resolveSparseRerankPoolLimit(finalLimit);
	const rerankQuery = useWorkersAIRerank
		? resolveHybridRerankQuery(denseReq, sparseReq, rerank?.params)
		: null;
	const shouldUseWorkersAIRerank = useWorkersAIRerank && !!rerankQuery;
	const isTrueHybrid = !!sparseReq;
	const denseTopK = clampVectorizeTopK(denseReq.limit ?? rerankPoolLimit);
	const sparseTopK = sparseReq?.limit ?? sparsePoolLimit;

	const denseVectorizeFilter = parsedDenseFilter.plan
		? compileFilterPlanToVectorize(parsedDenseFilter.plan)
		: undefined;

	const [{ results: denseResults }, sparseResults] = await Promise.all([
		queryDenseRankedResults(
			env,
			queryVector,
			collectionName,
			safeName,
			denseTopK,
			isTrueHybrid,
			denseVectorizeFilter,
		),
		querySparseRankedResults(
			env,
			safeName,
			queryText,
			sparseTopK,
			parsedSparseFilter.plan,
		),
	]);

	const fused = rrfFuse([denseResults, sparseResults], rrfK);
	const preRerankResults = shouldUseWorkersAIRerank
		? fused.slice(0, rerankPoolLimit)
		: fused.slice(0, finalLimit);
	const hydratedResults = await hydrateRankedResults(
		env,
		collectionName,
		safeName,
		preRerankResults,
	);
	const rerankedResults = shouldUseWorkersAIRerank
		? await rerankWithWorkersAI(env, hydratedResults, rerankQuery)
		: hydratedResults;
	const finalResults = shouldUseWorkersAIRerank
		? rerankedResults.slice(0, finalLimit)
		: rerankedResults;

	const results = finalResults.map((result) => ({
		id: result.id,
		distance: result.score,
		score: result.score,
		rrfScore: result.rrfScore,
		...extractEntityFieldsFromRecord(result.data),
	}));

	const filtered = outputFields?.length
		? filterFields(results, [
				"id",
				"distance",
				"score",
				"rrfScore",
				...outputFields,
			])
		: results;

	return ok(filtered);
}

```

### 8. worker-configuration.d.ts (lines 3916-4224)
```ts
// Filter types (shared with AutoRAG for compatibility)
type ComparisonFilter = {
	key: string;
	type: "eq" | "ne" | "gt" | "gte" | "lt" | "lte";
	value: string | number | boolean;
};
type CompoundFilter = {
	type: "and" | "or";
	filters: ComparisonFilter[];
};
// AI Search V2 Request Types
type AiSearchSearchRequest = {
	messages: Array<{
		role: "system" | "developer" | "user" | "assistant" | "tool";
		content: string | null;
	}>;
	ai_search_options?: {
		retrieval?: {
			retrieval_type?: "vector" | "keyword" | "hybrid";
			/** Match threshold (0-1, default 0.4) */
			match_threshold?: number;
			/** Maximum number of results (1-50, default 10) */
			max_num_results?: number;
			filters?: CompoundFilter | ComparisonFilter;
			/** Context expansion (0-3, default 0) */
			context_expansion?: number;
			[key: string]: unknown;
		};
		query_rewrite?: {
			enabled?: boolean;
			model?: string;
			rewrite_prompt?: string;
			[key: string]: unknown;
		};
		reranking?: {
			/** Enable reranking (default false) */
			enabled?: boolean;
			model?: "@cf/baai/bge-reranker-base" | "";
			/** Match threshold (0-1, default 0.4) */
			match_threshold?: number;
			[key: string]: unknown;
		};
		[key: string]: unknown;
	};
};
type AiSearchChatCompletionsRequest = {
	messages: Array<{
		role: "system" | "developer" | "user" | "assistant" | "tool";
		content: string | null;
	}>;
	model?: string;
	stream?: boolean;
	ai_search_options?: {
		retrieval?: {
			retrieval_type?: "vector" | "keyword" | "hybrid";
			match_threshold?: number;
			max_num_results?: number;
			filters?: CompoundFilter | ComparisonFilter;
			context_expansion?: number;
			[key: string]: unknown;
		};
		query_rewrite?: {
			enabled?: boolean;
			model?: string;
			rewrite_prompt?: string;
			[key: string]: unknown;
		};
		reranking?: {
			enabled?: boolean;
			model?: "@cf/baai/bge-reranker-base" | "";
			match_threshold?: number;
			[key: string]: unknown;
		};
		[key: string]: unknown;
	};
	[key: string]: unknown;
};
// AI Search V2 Response Types
type AiSearchSearchResponse = {
	search_query: string;
	chunks: Array<{
		id: string;
		type: string;
		/** Match score (0-1) */
		score: number;
		text: string;
		item: {
			timestamp?: number;
			key: string;
			metadata?: Record<string, unknown>;
		};
		scoring_details?: {
			/** Keyword match score (0-1) */
			keyword_score?: number;
			/** Vector similarity score (0-1) */
			vector_score?: number;
		};
	}>;
};
type AiSearchListResponse = Array<{
	id: string;
	internal_id?: string;
	account_id?: string;
	account_tag?: string;
	/** Whether the instance is enabled (default true) */
	enable?: boolean;
	type?: "r2" | "web-crawler";
	source?: string;
	[key: string]: unknown;
}>;
type AiSearchConfig = {
	/** Instance ID (1-32 chars, pattern: ^[a-z0-9_]+(?:-[a-z0-9_]+)*$) */
	id: string;
	type: "r2" | "web-crawler";
	source: string;
	source_params?: object;
	/** Token ID (UUID format) */
	token_id?: string;
	ai_gateway_id?: string;
	/** Enable query rewriting (default false) */
	rewrite_query?: boolean;
	/** Enable reranking (default false) */
	reranking?: boolean;
	embedding_model?: string;
	ai_search_model?: string;
};
type AiSearchInstance = {
	id: string;
	enable?: boolean;
	type?: "r2" | "web-crawler";
	source?: string;
	[key: string]: unknown;
};
// AI Search Instance Service - Instance-level operations
declare abstract class AiSearchInstanceService {
	/**
	 * Search the AI Search instance for relevant chunks.
	 * @param params Search request with messages and AI search options
	 * @returns Search response with matching chunks
	 */
	search(params: AiSearchSearchRequest): Promise<AiSearchSearchResponse>;
	/**
	 * Generate chat completions with AI Search context.
	 * @param params Chat completions request with optional streaming
	 * @returns Response object (if streaming) or chat completion result
	 */
	chatCompletions(
		params: AiSearchChatCompletionsRequest,
	): Promise<Response | object>;
	/**
	 * Delete this AI Search instance.
	 */
	delete(): Promise<void>;
}
// AI Search Account Service - Account-level operations
declare abstract class AiSearchAccountService {
	/**
	 * List all AI Search instances in the account.
	 * @returns Array of AI Search instances
	 */
	list(): Promise<AiSearchListResponse>;
	/**
	 * Get an AI Search instance by ID.
	 * @param name Instance ID

...3535 bytes truncated...

The tool call succeeded but the output was truncated. Full output saved to: /home/jaryl/.local/share/opencode/tool-output/tool_d0524456b001D3OGcYgdVpUyR0
Use the Task tool to have explore agent process this file with Grep and Read (with offset/limit). Do NOT read the full file yourself - delegate to save context.
```

---

## Assistant

## 1) Query: “where are Vectorize metadata filters compiled”
**Relevance:** yes  
**Top hits:**
- `src/search.ts` (imports/uses `compileFilterPlanToVectorize`): shows filter plan → `vectorizeFilter` passed into `env.VECTORIZE.query(..., filter: vectorizeFilter)` (around line ~1-120, and dense query call around ~330-380).
- `src/search.ts` (`compileFilterPlanToVectorize(...)`): `vectorizeFilter = parsedFilter.plan ? compileFilterPlanToVectorize(parsedFilter.plan) : undefined` (around line ~440).
- `src/search.ts` (dense query uses it): `env.VECTORIZE.query(..., ...(vectorizeFilter && { filter: vectorizeFilter }))` (around line ~180-240).
- `src/entities.ts` (filter compilation helpers imported): `compileFilterPlanToSql` etc. show the same filter-plan pipeline is shared/used in entities mutation paths (around top of file, ~1-40).

**Misses/caveats:** The semantic hits point to *where compilation is invoked*; the actual compiler implementation is in `src/filters.ts`, which didn’t show up in the top hits for this query (likely still present but not surfaced by the search).

---

## 2) Query: “how does collection drop restore deleted vectors”
**Relevance:** yes  
**Top hits:**
- `src/entities.ts` (`rollbackDeletedVectors`): documents the restore flow for Vectorize deletes—`restoreVectorRecords(...)` calls `env.VECTORIZE.upsert(vectors.slice(...))` (around `rollbackDeletedVectors`, ~120-190).
- `src/entities.ts` (`restoreVectorRecords`): upsert in chunks of 1000 (around ~150-170).
- `src/entities.ts` (`loadTrackedVectorRecords` / `loadTrackedVectorRecordsById` helpers): loads prior vectors to restore by mapping tracked logical IDs → physical IDs → `VECTORIZE.getByIds` (around ~200-250+).
- `test/collections.spec.ts` (drop rollback scenarios): tests that `dropCollection(...)` triggers delete/restore behavior (mocking `VECTORIZE.deleteByIds` and `VECTORIZE.upsert`) and discusses rollback failure modes (around ~592-874).

**Misses/caveats:** This is clearly the *rollback restore* logic, but the semantic hits don’t directly show the `dropCollection` orchestration point in a single file in the top results—best seen in `src/collections.ts` (not surfaced in these top hits).

---

## 3) Query: “where does Workers AI rerank get the query text”
**Relevance:** yes  
**Top hits:**
- `src/rerank.ts` (`resolveHybridRerankQuery`): chooses rerank query text from `rerank.params.query` first, else sparse request query (`extractRequestQuery`), else dense request string (`extractRequestQuery`) (around ~55-85).
- `src/rerank.ts` (`extractRequestQuery`): takes `req.data[0]` and normalizes/trim-checks it to a string (around ~20-45).
- `src/rerank.ts` (`rerankWithWorkersAI`): normalizes `query` again and then calls `env.AI.run(..., { query: normalizedQuery, contexts: ... })` (around ~86-105).
- `test/search.spec.ts` (“calls Workers AI rerank when query text exists”): asserts `e.AI.run(... expect.objectContaining({ query: "hello world" ... }))` (around the relevant test block ~2379-2644).
- `src/search.ts` (`resolveHybridRerankQuery(...)` wiring): `const rerankQuery = useWorkersAIRerank ? resolveHybridRerankQuery(denseReq, sparseReq, rerank?.params) : null;` then passes `rerankQuery` into `rerankWithWorkersAI(...)` (around ~470-520).

**Misses/caveats:** None obvious. The only caveat is that if `resolveHybridRerankQuery(...)` returns `null`, reranking is skipped (covered by tests and `rerankWithWorkersAI` guard).

---

