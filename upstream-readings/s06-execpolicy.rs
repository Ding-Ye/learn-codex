// =============================================================================
//  Upstream reading for s06 — execpolicy DSL
//  Source: codex-rs/core/src/exec_policy.rs    (integration with the loop)
//          codex-rs/execpolicy/src/lib.rs      (the full Starlark crate)
// =============================================================================

/// Source: codex-rs/core/src/exec_policy.rs
pub struct ExecPolicy {
    rules: Vec<PolicyRule>,
    heuristics: HeuristicsConfig,
}

pub enum PolicyRule {
    PrefixRule {
        prefix: Vec<String>,
        decision: RuleDecision,
        reason: Option<String>,
    },
    NetworkRule {
        host: String,
        protocol: String,
        decision: RuleDecision,
    },
    /// ★ This is where upstream gets its expressive power: a Starlark
    ///   function that takes the command and returns a decision.
    ScriptRule {
        starlark_fn: starlark::Value,
    },
}

pub enum RuleDecision {
    Allow,
    Prompt { reason: String },
    Forbidden { reason: String },
}

pub struct HeuristicsConfig {
    pub use_known_safe_list: bool,      // built-in allowlist (ls, cat, …)
    pub use_dangerous_patterns: bool,   // regex scan for rm -rf, dd, …
}

impl ExecPolicy {
    /// Returns the first matching rule, or None to fall back to heuristics +
    /// the caller's default.
    pub fn classify(&self, command: &[String]) -> Option<&PolicyRule> {
        for rule in &self.rules {
            match rule {
                PolicyRule::PrefixRule { prefix, .. } if prefix_matches(command, prefix) => {
                    return Some(rule);
                }
                PolicyRule::ScriptRule { starlark_fn } => {
                    let mut eval = starlark::Evaluator::new(/* … */);
                    let result = starlark_fn.call(/* command */);
                    if let Ok(decision) = result.try_into() {
                        return Some(rule); // (with synthesised decision)
                    }
                }
                PolicyRule::NetworkRule { host, protocol, .. } => {
                    // for cd-like / curl-like commands, parse out the URL
                    // and match host+protocol.
                }
                _ => {}
            }
        }
        if self.heuristics.use_known_safe_list && is_known_safe_command(command) {
            return Some(/* synthesised Allow rule */);
        }
        if self.heuristics.use_dangerous_patterns && command_might_be_dangerous(command) {
            return Some(/* synthesised Prompt rule */);
        }
        None
    }
}

// -----------------------------------------------------------------------------
// Source: codex-rs/execpolicy/src/lib.rs (the Starlark crate)
//
// Starlark policy file looks like:
//
//    def policy(cmd):
//        if cmd[0] == "git" and cmd[1] in ("status", "log", "diff"):
//            return Allow()
//        if cmd[0] == "rm":
//            return Prompt(reason="destructive")
//        if cmd[0] == "sudo":
//            return Forbidden(reason="privilege escalation")
//        return None  # fall through to defaults
//
// learn-codex's textual DSL collapses this to one line per case.
// -----------------------------------------------------------------------------

// =============================================================================
// Comparison summary
//
//   Concept              | Upstream                          | learn-codex (s06)
//   ---------------------+-----------------------------------+----------------------
//   rule kinds           | Prefix + Network + ScriptRule     | Prefix + Exact
//   pattern language     | Starlark Python-subset            | textual DSL (one line/rule)
//   match strategy       | first-match, w/ heuristics tail   | first-match, simple default
//   network rules        | host+protocol predicate           | (omitted)
//   heuristic fallback   | known-safe + dangerous detectors  | (omitted)
//   rule sources         | merged from 3 layers              | one file
//   integration          | maybe_ask_for_approval consults   | Trusted func pointer
// =============================================================================
