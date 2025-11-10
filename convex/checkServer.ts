"use node";

import { internalAction } from "./_generated/server";
import { api } from "./_generated/api";
import { status as mcStatus } from "minecraft-server-util";

const SERVER_HOST = process.env.SERVER_HOST!; 
const SERVER_PORT = parseInt(process.env.SERVER_PORT || "25565");
const TELEGRAM_BOT_TOKEN = process.env.TELEGRAM_BOT_TOKEN!;
const TELEGRAM_CHAT_ID = process.env.TELEGRAM_CHAT_ID!;

export const checkServer = internalAction({
    handler: async (ctx) => {
      const latest = await ctx.runQuery(api.status.getLatest);
  
      // Retry logic: try up to 3 times before marking as offline
      let online = false;
      const maxRetries = 3;
      const retryDelay = 3000; // 3 seconds between retries
      
      for (let attempt = 1; attempt <= maxRetries; attempt++) {
        try {
          const res = await mcStatus(SERVER_HOST, SERVER_PORT, { timeout: 3000 });
          online = !!res.version?.name;
          if (online) {
            break; // Success, no need to retry
          }
        } catch (error) {
          // If this is the last attempt, mark as offline
          if (attempt === maxRetries) {
            online = false;
            console.log(`Server check failed after ${maxRetries} attempts:`, error);
          } else {
            // Wait before retrying
            await new Promise(resolve => setTimeout(resolve, retryDelay));
            console.log(`Server check attempt ${attempt} failed, retrying...`);
          }
        }
      }
  
      const previous = latest?.online ?? null;
  
      await ctx.runMutation(api.status.insert, {
        online,
        lastChecked: Date.now(),
      });
  
      // if (previous !== null && previous !== online) {
      //   const text = online
      //     ? "✅ сервер онлайн!"
      //     : "⚠️ сервер офлайн((";
  
      //   await fetch(`https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage`, {
      //     method: "POST",
      //     headers: { "Content-Type": "application/json" },
      //     body: JSON.stringify({ chat_id: TELEGRAM_CHAT_ID, text }),
      //   });
      // }

      // Update chat title based on server status
      const chatTitle = online
        ? "🟢 lnudorm3 minecraft йоу"
        : "🔴 lnudorm3 minecraft йоу";

      await fetch(`https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/setChatTitle`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ chat_id: TELEGRAM_CHAT_ID, title: chatTitle }),
      });
  
      console.log(`Server status: ${online ? "online" : "offline"}`);
    },
  });
  
