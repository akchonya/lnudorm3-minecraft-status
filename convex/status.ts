import { query, mutation } from "./_generated/server";
import { v } from "convex/values";

export const getLatest = query(async (ctx) => {
  return await ctx.db.query("status").order("desc").first();
});

export const insert = mutation({
  args: {
    online: v.boolean(),
    lastChecked: v.number(),
  },
  handler: async (ctx, args) => {
    await ctx.db.insert("status", args);
  },
});
