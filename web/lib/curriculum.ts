// The locked curriculum from .learn/plan.md. SessionNav and the landing
// page both read from this single source of truth. Slugs match
// docs/{zh,en}/<slug>.md.
//
// "available: false" means the chapter exists in the curriculum but its
// docs aren't written yet — the link will render but go to a placeholder.

export type ChapterMeta = {
  slug: string;
  num: string; // "s01", "s02", "s_full", "M", "A", "B"
  title: { zh: string; en: string };
  available: boolean;
};

export const CURRICULUM: ChapterMeta[] = [
  {
    slug: "multi-model",
    num: "M",
    title: {
      zh: "多模型接入指南（OpenAI / Anthropic / Echo）",
      en: "Multi-model guide (OpenAI / Anthropic / Echo)",
    },
    available: true,
  },
  {
    slug: "s01-minimum-loop",
    num: "s01",
    title: {
      zh: "最小 agent loop：Op / EventMsg 协议",
      en: "Minimum loop: Op / EventMsg protocol",
    },
    available: true,
  },
  {
    slug: "s02-model-client",
    num: "s02",
    title: {
      zh: "调用 Chat Completions 流式 + tool-use",
      en: "Streaming Chat Completions + tool-use",
    },
    available: true,
  },
  {
    slug: "s03-exec-tool",
    num: "s03",
    title: {
      zh: "shell 执行：流式输出 + 截断 + 超时",
      en: "Shell exec: streaming + cap + timeout",
    },
    available: true,
  },
  {
    slug: "s04-apply-patch",
    num: "s04",
    title: {
      zh: "V4A patch DSL 解析 + 应用",
      en: "V4A patch DSL parser + applier",
    },
    available: true,
  },
  {
    slug: "s05-approval",
    num: "s05",
    title: {
      zh: "审批策略 + ExecApprovalRequest 往返",
      en: "Approval policy + ExecApprovalRequest round-trip",
    },
    available: true,
  },
  {
    slug: "s06-execpolicy",
    num: "s06",
    title: {
      zh: "命令安全 DSL → Allow / Prompt / Forbidden",
      en: "Shell-safety DSL → Allow / Prompt / Forbidden",
    },
    available: true,
  },
  {
    slug: "s07-rollout",
    num: "s07",
    title: {
      zh: "JSONL 会话日志 + resume from disk",
      en: "JSONL session log + resume from disk",
    },
    available: true,
  },
  {
    slug: "s08-sandbox",
    num: "s08",
    title: {
      zh: "macOS Seatbelt + Linux Landlock 适配",
      en: "macOS Seatbelt + Linux Landlock adapters",
    },
    available: true,
  },
  {
    slug: "s09-mcp-bridge",
    num: "s09",
    title: {
      zh: "stdio JSON-RPC 调用外部 MCP server",
      en: "stdio JSON-RPC client to external MCP server",
    },
    available: true,
  },
  {
    slug: "s10-cli-driver",
    num: "s10",
    title: {
      zh: "clap-style 子命令 + slash 命令 + 全集成",
      en: "clap-style subcommands + slash commands + glue",
    },
    available: true,
  },
  {
    slug: "s_full-integration",
    num: "s_full",
    title: { zh: "端到端集成：16 步执行轨迹", en: "End-to-end: 16-step trace" },
    available: true,
  },
  {
    slug: "appendix-a-mental-model",
    num: "A",
    title: {
      zh: "附录 A · 本地优先 + Responses-vs-Chat-Completions + peer 比较",
      en: "Appendix A · Local-first + Responses-vs-Chat-Completions + peer comparison",
    },
    available: true,
  },
  {
    slug: "appendix-b-upstream-map",
    num: "B",
    title: {
      zh: "附录 B · 上游 codex-rs 源码导读地图",
      en: "Appendix B · upstream codex-rs source-reading map",
    },
    available: true,
  },
];

export type Locale = "zh" | "en";

export function chapterTitle(c: ChapterMeta, locale: Locale): string {
  return c.title[locale];
}
