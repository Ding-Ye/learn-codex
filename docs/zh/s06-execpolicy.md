---
title: "s06 · execpolicy：命令安全 DSL"
chapter: 6
slug: s06-execpolicy
est_read_min: 9
---

# s06 · execpolicy：命令安全 DSL

> 教什么：把 s05 留下的 `Trusted` 函数指针实例化成一份**真的、可外部编辑**的规则文件。声明式安全策略好处：策略本身可以审计、可以版本化、用户和管理员可以分别维护。

---

## Problem / 问题

s05 的 Gate 设计上正确，但 `Trusted` 是个 Go 函数指针——意味着每次想加一条 "allow `git status`" 规则就得重新编译 codex。生产 codex 让用户在 `~/.codex/config.toml` 里配 execpolicy，运维可以分发一份 policy 文件管全公司，开发者本地再 override——这都需要一个真正的、可外部编辑的 DSL。

## Solution / 解决方案

最小可教学的语法：

```
# 注释行
default prompt
rule allow  prefix "git status"
rule prompt prefix "rm"   reason "destructive"
rule forbid prefix "sudo" reason "privilege escalation"
rule allow  exact  "go test ./..."
```

- 每行 `rule <decision> <match> "pattern" [reason "..."]`。
- `decision` ∈ `{allow, prompt, forbid}`。
- `match` ∈ `{prefix, exact}`。
- `pattern` 是引号包住的字符串，里面用空格分词。
- 第一个匹配的 rule 决定结果；没人匹配走 `default`。

我们**不**复刻上游的 Starlark：那是一种完整的脚本语言（标签、函数、列表 …）。这一节的目标是让你看到"DSL 不需要复杂"——一段 80 行 Go 解析器 + 一个第一匹配胜出的求值器，就能解决 99% 的 case。读完之后你回头读上游的 Starlark，直觉就有了。

## How It Works / 工作原理

```
.rules file ──▶ Parse(reader) ──▶ *Policy
                                     │
                                     │  Classify(command):
                                     │    for r in Rules:
                                     │      if r.matches(command): return r.Decision, r
                                     │    return Default, nil
                                     ▼
                              (Decision, *Rule)
                                     │
                                     ▼
                                wired into s05's Gate.Trusted:
                                   if Classify(cmd) == DecideAllow → Trusted=true
```

核心 25 行：

```go
// parser
case "rule":
    d, _ := parseDecision(fields[1])  // allow | prompt | forbid
    m, _ := parseMatch(fields[2])     // prefix | exact
    pat := fields[3]                  // already unquoted
    r := Rule{Decision: d, Match: m, Tokens: strings.Fields(pat)}
    if len(fields) >= 6 && fields[4] == "reason" { r.Reason = fields[5] }
    p.Rules = append(p.Rules, r)

// evaluator
func (p *Policy) Classify(cmd []string) (Decision, *Rule) {
    for i := range p.Rules {
        if p.Rules[i].matches(cmd) { return p.Rules[i].Decision, &p.Rules[i] }
    }
    return p.Default, nil
}

func (r *Rule) matches(cmd []string) bool {
    switch r.Match {
    case MatchExact:  return slicesEqual(cmd, r.Tokens)
    case MatchPrefix: return len(cmd) >= len(r.Tokens) && slicesEqual(cmd[:len(r.Tokens)], r.Tokens)
    }
    return false
}
```

**3 个非显然之处**：

1. **first-match wins**——文件顺序就是优先级。这意味着把 `forbid sudo` 放在 `allow git` 后面就行；不需要任何"评分"逻辑。
2. **`splitFields` 自己实现**——`bufio.Scanner` 的 split 不会处理引号。我们用一个状态机扫一遍，遇到 `"` 翻 `inQuote` 标志。
3. **不实现 longest-prefix-wins**——上游 Starlark policy 会在多条规则同时匹配时选 token 数最多的。我们故意不做：让规则文件作者按语义顺序写，可读性更高。

## What Changed / 与 s05 的变化

```diff
+ type Decision int     // Allow | Prompt | Forbidden
+ type Match int        // Prefix | Exact
+ type Rule struct { Decision; Match; Tokens []string; Reason string }
+ type Policy struct { Rules []Rule; Default Decision }
+
+ func Parse(r io.Reader) (*Policy, error)
+ func (p *Policy) Classify(command []string) (Decision, *Rule)

  // s05 had:
  // type Trusted func(command []string) (allowed bool, reason string)
  //
  // s06 fills it:
  trusted := func(cmd []string) (bool, string) {
+    d, r := policy.Classify(cmd)
+    if d == DecideAllow { return true, "" }
+    reason := ""
+    if r != nil { reason = r.Reason }
+    return false, reason
  }
  gate.Trusted = trusted
```

## Try It / 动手试一试

```bash
cd agents/s06-execpolicy

go run ./cmd testdata/default.rules -- git status
# decision=allow ...

go run ./cmd testdata/default.rules -- rm -rf /tmp
# decision=prompt reason="destructive"

go run ./cmd testdata/default.rules -- sudo whatever
# decision=forbid reason="privilege escalation"

go test -v ./...
```

## Upstream Source Reading / 上游源码阅读

```upstream:codex-rs/core/src/exec_policy.rs
// Source: codex-rs/core/src/exec_policy.rs (节选)
// 上游真正的 DSL 在 codex-rs/execpolicy/，是一个完整的 Starlark 解释器。
// 这里只展示 core 怎么把它接上去。

pub struct ExecPolicy {
    rules: Vec<PolicyRule>,
    heuristics: HeuristicsConfig,   // is_known_safe / might_be_dangerous
}

pub enum PolicyRule {
    PrefixRule { prefix: Vec<String>, decision: RuleDecision, reason: Option<String> },
    NetworkRule { host: String, protocol: String, decision: RuleDecision },
    ScriptRule { starlark_fn: starlark::Value },   // ← full Starlark predicate
}

pub enum RuleDecision { Allow, Prompt { reason: String }, Forbidden { reason: String } }

impl ExecPolicy {
    pub fn classify(&self, command: &[String]) -> Option<&PolicyRule> {
        // 1. iterate rules in order
        // 2. for ScriptRule, invoke the Starlark predicate
        // 3. on first match, return the rule
        // 4. if no match, fall back to heuristics:
        //      - is_known_safe_command(cmd) → synthesize an Allow rule
        //      - command_might_be_dangerous(cmd) → synthesize a Prompt rule
        //      - otherwise return None → caller decides default
    }
}
```

**对照阅读要点**：

- **3 类规则 vs 我们的 2 类**：上游有 NetworkRule（按网络主机决策）和 ScriptRule（任意 Starlark 谓词）。学习版只做 prefix/exact——这 2 类已经覆盖大部分日常 case。
- **heuristics fallback**：上游有内置启发式（已知安全命令清单 + 危险命令模式扫描）。学习版纯 explicit。
- **rule 来源多层叠加**：上游会从 `~/.codex/config.toml`、组织级 policy、`--profile` flag 三层 merge。学习版只读一个文件——但语义上完全等价。
- **Starlark vs 文本 DSL**：上游用 Starlark 一次到位，预算很高；学习版用文本 DSL，但延伸到 Starlark 是一行换 parser 的事。

**想读更多**：从 `codex-rs/execpolicy/src/lib.rs` 入手看 Starlark `eval` 怎么调，跟着 `RuleDecision` 进 `core/src/codex_thread.rs::maybe_ask_for_approval` 看决策怎么进入 approval 流程。这条线是 s05 → s06 → s08 的代码地图。

---

**下一节预告**：s07 给 agent 加上耐用性——所有 submit / event / tool call 落 JSONL 文件，下次启动可以从中恢复整个会话。
