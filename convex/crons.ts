import { cronJobs } from "convex/server";
import { internal } from "./_generated/api";

const crons = cronJobs();

crons.interval(
  "check minecraft server",
  { seconds: 30 },
  internal.checkServer.checkServer
);

export default crons;
