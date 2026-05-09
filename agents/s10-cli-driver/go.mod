module github.com/Ding-Ye/learn-codex/agents/s10-cli-driver

go 1.24

require (
	github.com/Ding-Ye/learn-codex/agents/s02-model-client v0.0.0
	github.com/Ding-Ye/learn-codex/agents/s07-rollout v0.0.0
)

replace (
	github.com/Ding-Ye/learn-codex/agents/s02-model-client => ../s02-model-client
	github.com/Ding-Ye/learn-codex/agents/s07-rollout => ../s07-rollout
)
