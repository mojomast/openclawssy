---
name: frontend-worker
description: Builds and migrates React dashboard pages with Vite, Tailwind, shadcn/ui, and Zustand
---

# Frontend Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use for features involving:
- React project setup and build configuration
- Creating or migrating dashboard pages to React
- Building React components (shared library, page-specific)
- Tailwind CSS / shadcn/ui styling work
- Zustand store creation and management
- Playwright e2e test creation for dashboard pages
- Vite build configuration and optimization

## Work Procedure

### 1. Understand the Feature

Read the feature description, preconditions, expectedBehavior, and verificationSteps carefully. Read `mission.md` and `.factory/library/` for architectural context.

For **migration** features: Read the original vanilla JS page file in `internal/channels/dashboard/ui/src/pages/` to understand every interaction, API call, data flow, and edge case. The React version must be **feature-complete** — every button, form, toggle, data display, real-time behavior, and keyboard shortcut must be preserved.

For **new** features: Read the API endpoints the page will consume (check `internal/channels/dashboard/handler.go` or the backend feature that created them).

### 2. Write Tests First (TDD)

Before writing any component code:

1. Create or update Playwright e2e test files in `internal/channels/dashboard/ui/tests/e2e/`
2. Write test cases covering the key interactions described in expectedBehavior
3. Use Playwright API route mocking (like existing tests in `qa_scripts.spec.js`) to mock backend responses
4. Tests should verify: page renders, navigation works, forms submit, data displays correctly, error states show
5. Run the tests to confirm they fail (red phase): `cd internal/channels/dashboard/ui && npx playwright test tests/e2e/<file>.spec.js`

### 3. Implement the React Components

Build the feature following these conventions:

**Project structure:**
```
internal/channels/dashboard/ui/
├── src/
│   ├── components/     # Shared components (Layout, DataTable, etc.)
│   ├── pages/          # Page components (one per route)
│   ├── stores/         # Zustand stores
│   ├── hooks/          # Custom React hooks
│   ├── lib/            # Utilities (API client, helpers)
│   └── App.tsx         # Router and top-level layout
├── index.html
├── vite.config.ts
├── tailwind.config.js
├── tsconfig.json
└── package.json
```

**Conventions:**
- Use TypeScript for all new React code (.tsx/.ts)
- Use shadcn/ui components as the base (Button, Card, Input, Select, Table, Dialog, etc.)
- Use Zustand for state that crosses component boundaries
- Use React Router with hash-based routing (HashRouter) for go:embed compatibility
- API calls go through a centralized API client in `src/lib/api.ts`
- Use `useSWR` or `useEffect` + Zustand for data fetching patterns
- Follow existing naming: PascalCase for components, camelCase for hooks/utils
- Keep components focused — extract sub-components when a file exceeds ~200 lines

**For migrations:** Match EVERY interaction from the original vanilla JS. Check the interaction catalog in `.factory/library/dashboard-interactions.md` if available. Missing interactions = incomplete migration.

### 4. Verify the Build

1. Run TypeScript check: `cd internal/channels/dashboard/ui && npx tsc --noEmit`
2. Run Vite build: `cd internal/channels/dashboard/ui && npx vite build`
3. Verify build output exists and is reasonable size
4. If this feature affects Go embed: run `go build -o ./bin/openclawssy ./cmd/openclawssy` from repo root

### 5. Run Tests (Green Phase)

1. Run your Playwright tests: `cd internal/channels/dashboard/ui && npx playwright test tests/e2e/<file>.spec.js`
2. All tests must pass
3. Run the full e2e suite to check for regressions: `cd internal/channels/dashboard/ui && npx playwright test`
   - If mission guidance documents known unrelated full-suite failures, run scoped specs for your feature and explicitly cite the known unrelated failures in handoff evidence.

### 6. Manual Verification

For each key interaction in the feature:
1. Start the dev server if not running: `cd internal/channels/dashboard/ui && npx vite --port 5175`
2. Open the page in a browser and verify the interaction works
3. Check browser console for errors
4. Check network tab for correct API calls
5. Record each check as an `interactiveChecks` entry

If mission guidance explicitly allows non-interactive/manual-skip validation for the current feature, perform equivalent evidence-backed checks (targeted automated flow + logs/screenshots/network artifacts) and document the substitution in `interactiveChecks`.

### 7. Cleanup

- Stop any dev servers you started
- Ensure no orphaned processes
- Run `go vet ./...` if you touched Go files

## Example Handoff

```json
{
  "salientSummary": "Migrated the Secrets page to React with shadcn/ui components. Created SecretsPage.tsx with key list (search, copy, delete), write-only secret form, and naming conventions panel. Added Playwright tests covering CRUD operations and form validation. All 6 e2e tests pass; verified manually that key filter, copy-to-clipboard, and delete confirmation work correctly.",
  "whatWasImplemented": "SecretsPage.tsx component with SecretKeyList, AddSecretForm, and NamingConventions sub-components. Zustand secretsStore for state management. 6 Playwright e2e tests in secrets.spec.ts. API client methods for GET/POST/DELETE /api/admin/secrets.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      { "command": "cd internal/channels/dashboard/ui && npx tsc --noEmit", "exitCode": 0, "observation": "No type errors" },
      { "command": "cd internal/channels/dashboard/ui && npx vite build", "exitCode": 0, "observation": "Build succeeded, 142KB gzipped" },
      { "command": "cd internal/channels/dashboard/ui && npx playwright test tests/e2e/secrets.spec.ts", "exitCode": 0, "observation": "6 tests passed" },
      { "command": "cd internal/channels/dashboard/ui && npx playwright test", "exitCode": 0, "observation": "All e2e tests pass, no regressions" }
    ],
    "interactiveChecks": [
      { "action": "Opened /dashboard/#/secrets, typed 'DISCORD' in filter", "observed": "Key list filtered to show only DISCORD_BOT_TOKEN" },
      { "action": "Clicked Copy on a key name", "observed": "Clipboard updated, 'Copied' toast appeared" },
      { "action": "Filled add-secret form and clicked Store Secret", "observed": "POST /api/admin/secrets fired, success message shown, key list refreshed" },
      { "action": "Clicked Delete on a key, confirmed dialog", "observed": "DELETE API called, key removed from list" }
    ]
  },
  "tests": {
    "added": [
      {
        "file": "internal/channels/dashboard/ui/tests/e2e/secrets.spec.ts",
        "cases": [
          { "name": "displays stored secret keys", "verifies": "Key list renders with mocked API data" },
          { "name": "filters keys by search input", "verifies": "Search input filters displayed keys" },
          { "name": "copies key name to clipboard", "verifies": "Copy button puts key name on clipboard" },
          { "name": "stores a new secret", "verifies": "Form submission POSTs to API" },
          { "name": "deletes a secret with confirmation", "verifies": "Delete flow with confirmation dialog" },
          { "name": "shows naming conventions", "verifies": "Conventions panel renders with Use Key buttons" }
        ]
      }
    ]
  },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

- A backend API endpoint this page depends on does not exist yet or returns unexpected shape
- The Vite/React build pipeline is broken and you cannot fix it
- A shared component needed by this page doesn't exist yet and is too large to build inline
- Go embed integration is broken after build changes
- Playwright cannot start or connect to the dev server
