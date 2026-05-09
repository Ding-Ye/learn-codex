---
title: "s06 · execpolicy: shell-safety DSL"
chapter: 6
slug: s06-execpolicy
est_read_min: 9
---

# s06 · execpolicy: shell-safety DSL

> What this teaches: instantiate s05's `Trusted` function pointer as a **real, externally editable** rule file. Declarative safety policies are auditable, version-controllable, and let users + admins maintain different layers.

---

## Problem

s05's Gate is correct in design, but `Trusted` is a Go func pointer — meaning every time you want to add `allow "git status"`, you have to recompile codex. Production codex lets users configure execpolicy in `~/.codex/config.toml`; ops can ship one policy file across the org while developers override locally. That requires a real, externally editable DSL.

## Solution

Minimal teachable grammar:

```
# comment line
default prompt
rule allow  prefix "git status"
rule prompt prefix "rm"   reason "destructive"
rule forbid prefix "sudo" reason "privilege escalation"
rule allow  exact  "go test ./..."
```

- Each line: `rule <decision> <match> "pattern" [reason "..."]`.
- `decision` ∈ `{allow, prompt, forbid}`.
- `match` ∈ `{prefix, exact}`.
- `pattern` is one quoted string, whitespace-tokenised inside.
- First matching rule wins; no match → `default`.

We **don't** replicate upstream's Starlark-based version: that's a full scripting language (lambdas, lists, predicates …). The point of this chapter is "DSLs don't need to be complicated" — 80 LOC of Go parser + a first-match evaluator covers 99% of cases. After this, reading upstream's Starlark policy will feel obvious.

## How It Works

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

Core 25 lines:

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

**3 non-obvious points**:

1. **First-match wins.** File order is priority order. So putting `forbid sudo` after `allow git` is fine; no scoring needed.
2. **`splitFields` is hand-rolled.** `bufio.Scanner`'s split functions don't handle quotes. We run a small state machine that flips an `inQuote` flag on `"`.
3. **No longest-prefix-wins.** Upstream's Starlark policy resolves ties by selecting the rule with the most tokens. We deliberately don't: rule files are easier to read when order is the ordering.

## What Changed (vs. s05)

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

## Try It

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

## Upstream Source Reading

```upstream:codex-rs/core/src/exec_policy.rs
// Source: codex-rs/core/src/exec_policy.rs (excerpt)
// The real DSL lives in codex-rs/execpolicy/, a full Starlark interpreter.
// This file shows how core wires it up.

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
        // 3. first match wins, return the rule
        // 4. if no match, fall back to heuristics:
        //      - is_known_safe_command(cmd) → synthesize an Allow rule
        //      - command_might_be_dangerous(cmd) → synthesize a Prompt rule
        //      - otherwise return None → caller decides default
    }
}
```

**Reading notes**:

- **3 rule kinds vs our 2.** Upstream has NetworkRule (host-based) and ScriptRule (arbitrary Starlark predicate). Teaching version uses prefix/exact only — covers 99% of common cases.
- **Heuristics fallback.** Upstream has built-in lists of known-safe + maybe-dangerous patterns. Teaching version is purely explicit.
- **Multi-layer rule sources.** Upstream merges `~/.codex/config.toml`, an org policy file, and `--profile` flags. Teaching version reads one file — but the semantics are the same.
- **Starlark vs textual DSL.** Upstream goes all-in on Starlark; the budget is high. Teaching version uses a textual DSL — extending it to Starlark is one parser swap.

**Read further**: start at `codex-rs/execpolicy/src/lib.rs` to see how Starlark `eval` is called, follow `RuleDecision` into `core/src/codex_thread.rs::maybe_ask_for_approval` to see how decisions flow into approval. That's the s05 → s06 → s08 trace.

---

**Next**: s07 makes the agent durable — every submit/event/tool call hits a JSONL file, and the next launch can resume the whole conversation from disk.
