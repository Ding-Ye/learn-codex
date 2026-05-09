import Link from "next/link";
import { notFound } from "next/navigation";
import { CURRICULUM, chapterTitle, type Locale } from "@/lib/curriculum";

export default async function Landing({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  if (locale !== "zh" && locale !== "en") notFound();
  const l = locale as Locale;

  const intro = l === "zh" ? INTRO_ZH : INTRO_EN;
  const ctaLabel = l === "zh" ? "从 s01 开始 →" : "Start at s01 →";

  return (
    <article className="prose-doc">
      <h1>learn-codex</h1>
      <p className="text-[var(--fg-muted)]">
        {l === "zh"
          ? "用 Go 从零渐进构建 openai/codex 的核心 10 个机制，每节末尾对照上游 Rust 源码。"
          : "Build openai/codex's 10 core mechanisms from scratch in Go, session by session — each chapter ends with the upstream Rust source."}
      </p>

      {intro.map((p, i) => (
        <p key={i}>{p}</p>
      ))}

      <p>
        <Link
          href={`/${l}/s/s01-minimum-loop`}
          className="inline-block mt-2 px-4 py-2 rounded border border-[var(--accent-soft)] hover:border-[var(--accent)]"
        >
          {ctaLabel}
        </Link>
      </p>

      <h2>{l === "zh" ? "课程" : "Curriculum"}</h2>
      <ul>
        {CURRICULUM.map((c) => (
          <li key={c.slug}>
            <span className="font-mono text-[var(--fg-muted)] mr-2">
              {c.num}
            </span>
            {c.available ? (
              <Link href={`/${l}/s/${c.slug}`}>{chapterTitle(c, l)}</Link>
            ) : (
              <span className="text-[var(--fg-muted)]">
                {chapterTitle(c, l)}{" "}
                <span className="text-xs">
                  ({l === "zh" ? "未发布" : "not yet"})
                </span>
              </span>
            )}
          </li>
        ))}
      </ul>
    </article>
  );
}

const INTRO_ZH = [
  "这个仓库的目标不是教你「用」codex，是教你「它怎么从零长出来」。",
  "每一节加一个机制——Op/EventMsg 协议、模型 client、shell 工具、apply-patch DSL、审批策略、execpolicy、rollout 持久化、Seatbelt/Landlock 沙盒、MCP 桥、CLI 集成——用 Go 写一份精简实现。看完十节，你会觉得 codex 不再是一团黑魔法。",
  "Go 实现是教学骨架，codex 上游是 Rust 实现。每节末尾的「上游源码阅读」把这两边对照起来，你能从 mini 版顺着指针读到 codex-rs 生产代码。",
];

const INTRO_EN = [
  "This repo's goal is not to teach you to *use* codex — it is to teach you how it grows from scratch.",
  "Each chapter adds one mechanism — the Op/EventMsg protocol, model client, shell tool, apply-patch DSL, approval policy, execpolicy, rollout persistence, Seatbelt/Landlock sandboxes, MCP bridge, CLI driver — implemented as a small Go module. After ten chapters, codex stops being black magic.",
  "Go is the teaching skeleton; codex-rs upstream is the production Rust implementation. The 'Upstream Source Reading' section at the end of every chapter bridges them — follow the pointers from the mini version straight into codex-rs production code.",
];
