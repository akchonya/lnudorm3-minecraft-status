import { defineSchema, defineTable } from "convex/server";
import { v } from "convex/values";

export default defineSchema({
  status: defineTable({
    online: v.boolean(),
    lastChecked: v.number(),
    players: v.optional(v.array(v.string())),
  }).index("by_lastChecked", ["lastChecked"]),
});

