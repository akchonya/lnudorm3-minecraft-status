"use node";

import { internalAction } from "./_generated/server";
import { api } from "./_generated/api";
import { status as mcStatus, queryFull as mcQueryFull } from "minecraft-server-util";

const SERVER_HOST = process.env.SERVER_HOST!; 
const SERVER_PORT = parseInt(process.env.SERVER_PORT || "25565");
const TELEGRAM_BOT_TOKEN = process.env.TELEGRAM_BOT_TOKEN!;
const TELEGRAM_CHAT_ID = process.env.TELEGRAM_CHAT_ID!;

export const checkServer = internalAction({
    handler: async (ctx) => {
      const latest = await ctx.runQuery(api.status.getLatest);
  
      const escapeHtml = (s: string) =>
        s
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;")
          .replace(/"/g, "&quot;")
          .replace(/'/g, "&#39;");
      const bold = (s: string) => `<b>${escapeHtml(s)}</b>`;

      const sendTelegramMessage = async (text: string) => {
        await fetch(`https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ chat_id: TELEGRAM_CHAT_ID, text, parse_mode: "HTML" }),
        });
      };

      // Retry logic: try up to 3 times before marking as offline
      let online = false;
      const maxRetries = 3;
      const retryDelay = 3000; // 3 seconds between retries
      let statusResponse: Awaited<ReturnType<typeof mcStatus>> | null = null;
      
      for (let attempt = 1; attempt <= maxRetries; attempt++) {
        try {
          const res = await mcStatus(SERVER_HOST, SERVER_PORT, { timeout: 3000 });
          statusResponse = res;
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
  
      const previousOnline = latest?.online ?? null;
      const previousPlayers = Array.isArray(latest?.players) ? latest.players : [];

      let currentPlayers = previousPlayers;
      let playerDataReliable = false;
      let playerCount: number | null = null;

      if (online) {
        let queryPlayers: string[] = [];

        try {
          const queryResult = await mcQueryFull(SERVER_HOST, SERVER_PORT, { timeout: 3000 });
          if (typeof queryResult.players?.online === "number") {
            playerCount = queryResult.players.online;
          }
          if (Array.isArray(queryResult.players?.list)) {
            queryPlayers = queryResult.players.list.filter((name): name is string => !!name);
          }
        } catch (error) {
          console.log("Full query failed, falling back to status sample:", error);
        }

        if (queryPlayers.length > 0) {
          currentPlayers = Array.from(new Set(queryPlayers));
          playerDataReliable = true;
        } else {
          // Try to use status ping info
          if (typeof statusResponse?.players?.online === "number") {
            playerCount = statusResponse.players.online;
          }
          if (statusResponse?.players?.sample) {
            currentPlayers = Array.from(
              new Set(
                statusResponse.players.sample
                  .map((player) => player.name)
                  .filter((name): name is string => !!name)
              )
            );
            playerDataReliable = currentPlayers.length > 0;
          }
        }

        // If we still don't have reliable names, use the count to avoid stale names
        if (!playerDataReliable) {
          if (playerCount === 0) {
            currentPlayers = [];
            // We can reliably say everyone left if reported count is 0.
            playerDataReliable = true;
          } else if (typeof playerCount === "number" && playerCount >= 0) {
            // Trim to the reported count to avoid keeping ghost players
            currentPlayers = previousPlayers.slice(0, playerCount);
          }
        }
      } else {
        currentPlayers = [];
        if (previousPlayers.length > 0) {
          playerDataReliable = true;
        }
      }

      const currentPlayerSet = new Set(currentPlayers);
      const previousPlayerSet = new Set(previousPlayers);

      const joinedPlayers = playerDataReliable
        ? currentPlayers.filter((player) => !previousPlayerSet.has(player))
        : [];
      const leftPlayers = playerDataReliable
        ? previousPlayers.filter((player) => !currentPlayerSet.has(player))
        : [];
  
      await ctx.runMutation(api.status.insert, {
        online,
        lastChecked: Date.now(),
        players: currentPlayers,
      });
  
      // if (previousOnline !== null && previousOnline !== online) {
      //   const text = online
      //     ? "✅ сервер онлайн!"
      //     : "⚠️ сервер офлайн((";

      //   await sendTelegramMessage(text);
      // }

      if (playerDataReliable && (joinedPlayers.length > 0 || leftPlayers.length > 0)) {
        const changes: string[] = [];

        if (joinedPlayers.length > 0) {
          if (joinedPlayers.length === 1) {
            changes.push(`😎 ${bold(joinedPlayers[0])} зайшов на сервер`);
          } else {
            changes.push(`😎 на сервер зайшли: ${joinedPlayers.map(bold).join(", ")}`);
          }
        }

        if (leftPlayers.length > 0) {
          if (leftPlayers.length === 1) {
            changes.push(`🥺 ${bold(leftPlayers[0])} вийшов`);
          } else {
            changes.push(`🥺 вийшли: ${leftPlayers.map(bold).join(", ")}`);
          }
        }

        if (changes.length > 0) {
          await sendTelegramMessage(changes.join("\n"));
        }
      }

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
  
