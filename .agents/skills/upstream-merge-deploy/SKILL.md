---
name: upstream-merge-deploy
description: Merge or sync upstream new-api changes while preserving this fork's custom features, audit omissions across backend and both frontends, handle conflicts, run validation, commit/push, and deploy to the production Docker Compose server. Use when the user asks to merge upstream, pull upstream, sync new upstream code, restore/preserve self-developed features after a merge, check for missing custom functionality, or update/deploy the v2 branch to the configured server.
---

# Upstream Merge And Deploy

Use this skill for recurring upstream merge/update work in this `new-api` fork.

Primary objective: **upstream changes must not remove, bypass, or silently degrade local custom features**. Treat the local custom feature set as protected behavior.

## Fixed Project Context

- Local repo: `D:\my_project\ct-new-api\new-api`
- Production server: `43.165.178.37`
- SSH host alias: `new-api-v2` (local `~/.ssh/config`, key `~/.ssh/id_ed25519_new_api`)
- Server deploy dir: `/root/new-api`
- Production compose service: `new-api`
- Production image tag used by compose: `calciumion/new-api:latest`
- Preferred branch for this fork: `v2`
- Frontend package manager: `bun`

Do not store or repeat server passwords. Connect with `ssh new-api-v2`. If key login fails, ask for the missing credential once.

## Protected Custom Feature Surfaces

Before and after each upstream merge, audit these surfaces explicitly:

- iPayNow top-up:
  - Backend: `/api/user/ipaynow/pay`, `/api/user/ipaynow/amount`, `IPayNowAdaptor`, `enable_ipaynow_topup`, `ipaynow_min_topup`
  - Default frontend wallet/top-up flow
  - Classic frontend top-up flow and `IPayNowQRModal`
- Bonus quota restrictions:
  - `bonus_quota_allowed_models`
  - `bonus_quota_validity_days`
  - UI text similar to `Limited to selected models: {{models}}`
- Email bind reward:
  - `email_bind_reward_enabled`
  - `email_bind_reward_quota`
  - account binding UI and i18n
- Frontend parity:
  - `web/default` and `web/classic` must both expose supported user-facing payment/account features unless a feature is intentionally theme-specific.
- Translation coverage:
  - `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`
  - classic i18n files when classic UI text changes.

Use `rg` for these symbols before concluding there are no omissions.

## Workflow

### 1. Preflight

1. Read the current project instructions if they are not already in context:
   - `AGENTS.md`
   - relevant skill files such as `i18n-translate` when translations are touched.
2. Check repo state:
   ```bash
   git status --short
   git branch --show-current
   git remote -v
   ```
3. If there are unrelated local changes, do not overwrite them. Work around them or ask only if they block the merge.
4. Identify the upstream source from remotes and user wording. Prefer existing remote names such as `upstream`; otherwise inspect `origin` and branches before deciding.

### 2. Merge Upstream Carefully

Fetch first:

```bash
git fetch --all --prune
```

When merging or rebasing:

- Do not use wholesale `ours`/`theirs` resolution on files touching protected custom feature surfaces.
- For every conflict, read both sides and preserve local custom behavior while integrating upstream fixes.
- If product semantics are ambiguous and cannot be inferred from code/tests, ask the user a focused question.
- After conflict resolution, run `git diff --check`.

### 3. Omission Audit

After the merge compiles, perform a protected-feature audit:

```bash
rg -n "ipaynow|IPayNow|enable_ipaynow_topup|ipaynow_min_topup|bonus_quota|bonus quota|email_bind_reward|Email Bind|Limited to selected models" controller router service model setting web i18n
```

Audit by behavior, not only string presence:

- Backend routes exist and point to working controllers.
- Settings are loaded and surfaced to `/api/status` when needed.
- `web/default` implements the feature using its React 19/Base UI/Tailwind conventions.
- `web/classic` implements equivalent behavior using its React 18/Semi conventions.
- Payment amount preview uses the correct provider-specific endpoint.
- Minimum top-up validation uses the selected payment method's `min_topup`, not a global fallback that breaks other payment methods.
- All new or restored frontend strings are translated.

If a protected feature exists in only one frontend, either port it or document why it is intentionally absent.

### 4. Validation

Run the smallest reliable validation set first, then broaden when the merge touched shared code.

Default validation commands:

```bash
git diff --check
go test ./controller -run 'Test.*IPayNow|TestPaymentWebhookAvailability'
cd web/default && bun run i18n:sync
cd web/default && bun run typecheck
cd web/default && bun run build
cd web/classic && bun run build
```

If backend shared relay, billing, model, router, or migration code changed, also run:

```bash
go test ./...
```

If frontend UI behavior changed materially, run a browser smoke check against the relevant local dev server or built preview.

### 5. Commit And Push

Before committing:

```bash
git status --short
git diff --stat
git diff --check
```

Commit only the intended files. Use a clear message such as:

```bash
git commit -m "fix: preserve custom features after upstream merge"
```

Push the active fork branch, usually:

```bash
git push origin v2
```

### 6. Production Deployment

Deploy only when the user explicitly asks to update/deploy the server, or when the current request includes that expectation.

On the server via `ssh new-api-v2`:

```bash
ssh new-api-v2
cd /root/new-api
git status --short
git branch --show-current
git fetch origin
git pull --ff-only
docker compose ps
```

Important:

- Preserve the server's existing `docker-compose.yml`, env files, volumes, and database credentials.
- Do not replace database credentials with local/default values.
- If compose uses `image:` without `build:`, rebuild the image with the same tag instead of editing compose.

Back up the currently deployed image before replacing it:

```bash
backup_tag="calciumion/new-api:backup-$(date +%Y%m%d%H%M%S)"
docker tag calciumion/new-api:latest "$backup_tag"
```

Build and restart only the app service:

```bash
docker build -t calciumion/new-api:latest .
docker compose up -d --no-deps --force-recreate new-api
```

Validate:

```bash
docker compose ps
docker compose logs --tail=120 new-api
curl -fsS http://127.0.0.1:3000/api/status || curl -fsS http://127.0.0.1:3000/
git log -1 --oneline
git status --short
```

If the new container fails to start or health checks fail, roll back immediately:

```bash
docker tag "$backup_tag" calciumion/new-api:latest
docker compose up -d --no-deps --force-recreate new-api
docker compose ps
docker compose logs --tail=120 new-api
```

Report whether rollback was needed.

## Final Report

Keep the final response concise and include:

- Local commit hash and branch.
- Whether protected custom feature audit passed.
- Validation commands run and outcomes.
- Deployment status, server path, container health, and API status result when deployed.
- Backup image tag when a production deploy occurred.
- Any remaining risks or commands that could not be run.

Never expose server passwords or database credentials in the final response.
