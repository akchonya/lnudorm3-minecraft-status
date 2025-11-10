import { query, mutation, internalMutation } from "./_generated/server";
import { v } from "convex/values";

const ONE_DAY_IN_MS = 24 * 60 * 60 * 1000;
const CLEANUP_BATCH_SIZE = 100;
const MAX_BATCHES_PER_INVOCATION = 30;

export const getLatest = query(async (ctx) => {
  return await ctx.db.query("status").order("desc").first();
});

export const insert = mutation({
  args: {
    online: v.boolean(),
    lastChecked: v.number(),
    players: v.optional(v.array(v.string())),
  },
  handler: async (ctx, args) => {
    const { players = [], ...rest } = args;
    await ctx.db.insert("status", { ...rest, players });
  },
});

export const cleanupOld = internalMutation({
  args: {},
  handler: async (ctx) => {
    const cutoff = Date.now() - ONE_DAY_IN_MS;
    for (let batch = 0; batch < MAX_BATCHES_PER_INVOCATION; batch++) {
      const oldEntries = await ctx.db
        .query("status")
        .withIndex("by_lastChecked", (q) => q.lt("lastChecked", cutoff))
        .take(CLEANUP_BATCH_SIZE);

      if (oldEntries.length === 0) {
        break;
      }

      for (const entry of oldEntries) {
        await ctx.db.delete(entry._id);
      }
    }
  },
});
