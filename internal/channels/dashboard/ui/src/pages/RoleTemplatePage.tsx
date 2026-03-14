import { useCallback, useEffect, useMemo, useState } from "react"
import { ApiError, api } from "@/lib/api"
import { useToast } from "@/hooks/useToast"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type RoleTemplate = {
  name: string
  description: string
  allowedTools: string[]
  deniedTools: string[]
  maxIterations: number
  timeoutMS: number
  memoryAccessScope: string
  writablePaths: string[]
  promptContract: string
  outputSchema: Record<string, unknown>
  handoffSchema: Record<string, unknown>
  escalationRules: string[]
  delegationPermissions: string[]
  isBuiltin: boolean
}

type RoleFormState = {
  name: string
  description: string
  allowedTools: string
  deniedTools: string
  maxIterations: string
  timeoutMS: string
  memoryAccessScope: string
  writablePaths: string
  promptContract: string
  outputSchema: string
  handoffSchema: string
  escalationRules: string
  delegationPermissions: string
}

const EMPTY_JSON = "{}"

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function asPositiveInt(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed < 0) {
    return 0
  }
  return Math.round(parsed)
}

function normalizeStringList(value: unknown): string[] {
  const items = Array.isArray(value) ? value : []
  const seen = new Set<string>()
  const out: string[] = []

  for (const item of items) {
    const trimmed = asText(item).trim()
    if (!trimmed || seen.has(trimmed)) {
      continue
    }
    seen.add(trimmed)
    out.push(trimmed)
  }
  return out
}

function parseRoleTemplate(value: unknown): RoleTemplate | null {
  const row = asRecord(value)
  if (!row) {
    return null
  }

  const name = asText(row.name).trim().toLowerCase()
  if (!name) {
    return null
  }

  return {
    name,
    description: asText(row.description),
    allowedTools: normalizeStringList(row.allowed_tools),
    deniedTools: normalizeStringList(row.denied_tools),
    maxIterations: asPositiveInt(row.max_iterations),
    timeoutMS: asPositiveInt(row.timeout_ms),
    memoryAccessScope: asText(row.memory_access_scope).trim(),
    writablePaths: normalizeStringList(row.writable_paths),
    promptContract: asText(row.prompt_contract),
    outputSchema: asRecord(row.output_schema) || {},
    handoffSchema: asRecord(row.handoff_schema) || {},
    escalationRules: normalizeStringList(row.escalation_rules),
    delegationPermissions: normalizeStringList(row.delegation_permissions),
    isBuiltin: Boolean(row.is_builtin),
  }
}

function formatJSON(value: Record<string, unknown>): string {
  try {
    return JSON.stringify(value || {}, null, 2)
  } catch {
    return EMPTY_JSON
  }
}

function parseErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const details = asRecord(error.details)
    const nested = asRecord(details?.error)
    const nestedMessage = asText(nested?.message).trim()
    if (nestedMessage) {
      return nestedMessage
    }
    const detailsMessage = asText(details?.message).trim()
    if (detailsMessage) {
      return detailsMessage
    }
  }

  if (error instanceof Error) {
    return error.message || "Unknown error"
  }
  return asText(error).trim() || "Unknown error"
}

function emptyRoleForm(): RoleFormState {
  return {
    name: "",
    description: "",
    allowedTools: "",
    deniedTools: "",
    maxIterations: "10",
    timeoutMS: "60000",
    memoryAccessScope: "read_only",
    writablePaths: "",
    promptContract: "",
    outputSchema: EMPTY_JSON,
    handoffSchema: EMPTY_JSON,
    escalationRules: "",
    delegationPermissions: "",
  }
}

function roleToForm(role: RoleTemplate): RoleFormState {
  return {
    name: role.name,
    description: role.description,
    allowedTools: role.allowedTools.join(", "),
    deniedTools: role.deniedTools.join(", "),
    maxIterations: String(role.maxIterations),
    timeoutMS: String(role.timeoutMS),
    memoryAccessScope: role.memoryAccessScope,
    writablePaths: role.writablePaths.join(", "),
    promptContract: role.promptContract,
    outputSchema: formatJSON(role.outputSchema),
    handoffSchema: formatJSON(role.handoffSchema),
    escalationRules: role.escalationRules.join(", "),
    delegationPermissions: role.delegationPermissions.join(", "),
  }
}

function parseListInput(value: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of value.split(/[\n,]/g)) {
    const trimmed = item.trim()
    if (!trimmed || seen.has(trimmed)) {
      continue
    }
    seen.add(trimmed)
    out.push(trimmed)
  }
  return out
}

function formToPayload(form: RoleFormState): Record<string, unknown> {
  const outputSchema = JSON.parse(form.outputSchema || EMPTY_JSON) as Record<string, unknown>
  const handoffSchema = JSON.parse(form.handoffSchema || EMPTY_JSON) as Record<string, unknown>

  return {
    name: form.name.trim().toLowerCase(),
    description: form.description.trim(),
    allowed_tools: parseListInput(form.allowedTools),
    denied_tools: parseListInput(form.deniedTools),
    max_iterations: Math.max(0, Number.parseInt(form.maxIterations || "0", 10) || 0),
    timeout_ms: Math.max(0, Number.parseInt(form.timeoutMS || "0", 10) || 0),
    memory_access_scope: form.memoryAccessScope.trim(),
    writable_paths: parseListInput(form.writablePaths),
    prompt_contract: form.promptContract.trim(),
    output_schema: outputSchema,
    handoff_schema: handoffSchema,
    escalation_rules: parseListInput(form.escalationRules),
    delegation_permissions: parseListInput(form.delegationPermissions),
  }
}

function RoleFormFields({
  prefix,
  form,
  disabled,
  onChange,
  includeName,
}: {
  prefix: "create" | "edit"
  form: RoleFormState
  disabled: boolean
  onChange: (key: keyof RoleFormState, value: string) => void
  includeName: boolean
}) {
  return (
    <div className="grid gap-3 md:grid-cols-2">
      {includeName ? (
        <label className="space-y-1 text-sm md:col-span-1">
          <span>Name</span>
          <Input
            data-testid={`${prefix}-role-name`}
            value={form.name}
            disabled={disabled}
            onChange={(event) => onChange("name", event.target.value)}
            placeholder="qa-specialist"
          />
        </label>
      ) : (
        <label className="space-y-1 text-sm md:col-span-1">
          <span>Name</span>
          <Input data-testid={`${prefix}-role-name`} value={form.name} disabled />
        </label>
      )}

      <label className="space-y-1 text-sm md:col-span-1">
        <span>Memory access scope</span>
        <Input
          data-testid={`${prefix}-role-memory-access-scope`}
          value={form.memoryAccessScope}
          disabled={disabled}
          onChange={(event) => onChange("memoryAccessScope", event.target.value)}
          placeholder="read_only"
        />
      </label>

      <label className="space-y-1 text-sm md:col-span-2">
        <span>Description</span>
        <textarea
          data-testid={`${prefix}-role-description`}
          className="min-h-16 w-full rounded-md border bg-background px-3 py-2 text-sm"
          value={form.description}
          disabled={disabled}
          onChange={(event) => onChange("description", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Allowed tools (comma/newline separated)</span>
        <textarea
          data-testid={`${prefix}-role-allowed-tools`}
          className="min-h-20 w-full rounded-md border bg-background px-3 py-2 text-sm"
          value={form.allowedTools}
          disabled={disabled}
          onChange={(event) => onChange("allowedTools", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Denied tools (comma/newline separated)</span>
        <textarea
          data-testid={`${prefix}-role-denied-tools`}
          className="min-h-20 w-full rounded-md border bg-background px-3 py-2 text-sm"
          value={form.deniedTools}
          disabled={disabled}
          onChange={(event) => onChange("deniedTools", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Max iterations</span>
        <Input
          data-testid={`${prefix}-role-max-iterations`}
          type="number"
          value={form.maxIterations}
          disabled={disabled}
          onChange={(event) => onChange("maxIterations", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Timeout (ms)</span>
        <Input
          data-testid={`${prefix}-role-timeout-ms`}
          type="number"
          value={form.timeoutMS}
          disabled={disabled}
          onChange={(event) => onChange("timeoutMS", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm md:col-span-2">
        <span>Writable paths (comma/newline separated)</span>
        <textarea
          data-testid={`${prefix}-role-writable-paths`}
          className="min-h-16 w-full rounded-md border bg-background px-3 py-2 text-sm"
          value={form.writablePaths}
          disabled={disabled}
          onChange={(event) => onChange("writablePaths", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm md:col-span-2">
        <span>Prompt contract</span>
        <textarea
          data-testid={`${prefix}-role-prompt-contract`}
          className="min-h-20 w-full rounded-md border bg-background px-3 py-2 text-sm"
          value={form.promptContract}
          disabled={disabled}
          onChange={(event) => onChange("promptContract", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Output schema (JSON)</span>
        <textarea
          data-testid={`${prefix}-role-output-schema`}
          className="min-h-24 w-full rounded-md border bg-background px-3 py-2 font-mono text-xs"
          value={form.outputSchema}
          disabled={disabled}
          onChange={(event) => onChange("outputSchema", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Handoff schema (JSON)</span>
        <textarea
          data-testid={`${prefix}-role-handoff-schema`}
          className="min-h-24 w-full rounded-md border bg-background px-3 py-2 font-mono text-xs"
          value={form.handoffSchema}
          disabled={disabled}
          onChange={(event) => onChange("handoffSchema", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Escalation rules (comma/newline separated)</span>
        <textarea
          data-testid={`${prefix}-role-escalation-rules`}
          className="min-h-16 w-full rounded-md border bg-background px-3 py-2 text-sm"
          value={form.escalationRules}
          disabled={disabled}
          onChange={(event) => onChange("escalationRules", event.target.value)}
        />
      </label>

      <label className="space-y-1 text-sm">
        <span>Delegation permissions (comma/newline separated)</span>
        <textarea
          data-testid={`${prefix}-role-delegation-permissions`}
          className="min-h-16 w-full rounded-md border bg-background px-3 py-2 text-sm"
          value={form.delegationPermissions}
          disabled={disabled}
          onChange={(event) => onChange("delegationPermissions", event.target.value)}
        />
      </label>
    </div>
  )
}

export function RoleTemplatePage() {
  const { toast } = useToast()

  const [roles, setRoles] = useState<RoleTemplate[]>([])
  const [selectedRoleName, setSelectedRoleName] = useState("")

  const [createForm, setCreateForm] = useState<RoleFormState>(() => emptyRoleForm())
  const [editForm, setEditForm] = useState<RoleFormState>(() => emptyRoleForm())

  const [loading, setLoading] = useState(true)
  const [loadingError, setLoadingError] = useState("")

  const [createError, setCreateError] = useState("")
  const [editError, setEditError] = useState("")

  const [creating, setCreating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)

  const loadRoles = useCallback(async (preferredRoleName = "") => {
    setLoading(true)
    setLoadingError("")
    try {
      const payload = await api.get<{ roles?: unknown }>("/api/admin/roles")
      const nextRoles = Array.isArray(payload.roles)
        ? payload.roles.map(parseRoleTemplate).filter((role): role is RoleTemplate => role !== null)
        : []

      setRoles(nextRoles)
      const fallback = nextRoles[0]?.name || ""
      const selected = preferredRoleName && nextRoles.some((role) => role.name === preferredRoleName)
        ? preferredRoleName
        : fallback
      setSelectedRoleName(selected)

      const selectedRole = nextRoles.find((role) => role.name === selected)
      setEditForm(selectedRole ? roleToForm(selectedRole) : emptyRoleForm())
    } catch (error) {
      setRoles([])
      setSelectedRoleName("")
      setEditForm(emptyRoleForm())
      setLoadingError(`Failed to load role templates: ${parseErrorMessage(error)}`)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadRoles()
  }, [loadRoles])

  const selectedRole = useMemo(
    () => roles.find((role) => role.name === selectedRoleName) || null,
    [roles, selectedRoleName]
  )

  const onSelectRole = useCallback((name: string) => {
    const role = roles.find((entry) => entry.name === name)
    setSelectedRoleName(name)
    setEditError("")
    setDeleteDialogOpen(false)
    setEditForm(role ? roleToForm(role) : emptyRoleForm())
  }, [roles])

  const updateCreateForm = useCallback((key: keyof RoleFormState, value: string) => {
    setCreateError("")
    setCreateForm((current) => ({ ...current, [key]: value }))
  }, [])

  const updateEditForm = useCallback((key: keyof RoleFormState, value: string) => {
    setEditError("")
    setEditForm((current) => ({ ...current, [key]: value }))
  }, [])

  const createRole = useCallback(async () => {
    setCreateError("")
    setCreating(true)
    try {
      const payload = formToPayload(createForm)
      const roleName = asText(payload.name).trim().toLowerCase()
      await api.post("/api/admin/roles", payload)
      toast({ title: "Role template created", description: `Created custom role ${roleName}.` })
      setCreateForm(emptyRoleForm())
      await loadRoles(roleName)
    } catch (error) {
      setCreateError(parseErrorMessage(error))
    } finally {
      setCreating(false)
    }
  }, [createForm, loadRoles, toast])

  const saveRole = useCallback(async () => {
    if (!selectedRole) {
      return
    }
    setEditError("")
    setSaving(true)
    try {
      const payload = formToPayload(editForm)
      await api.put(`/api/admin/roles/${encodeURIComponent(selectedRole.name)}`, payload)
      toast({ title: "Role template updated", description: `Updated custom role ${selectedRole.name}.` })
      await loadRoles(selectedRole.name)
    } catch (error) {
      setEditError(parseErrorMessage(error))
    } finally {
      setSaving(false)
    }
  }, [editForm, loadRoles, selectedRole, toast])

  const deleteRole = useCallback(async () => {
    if (!selectedRole) {
      return
    }
    setEditError("")
    setDeleting(true)
    try {
      await api.delete(`/api/admin/roles/${encodeURIComponent(selectedRole.name)}`)
      toast({ title: "Role template deleted", description: `Deleted custom role ${selectedRole.name}.` })
      setDeleteDialogOpen(false)
      await loadRoles()
    } catch (error) {
      setEditError(parseErrorMessage(error))
    } finally {
      setDeleting(false)
    }
  }, [loadRoles, selectedRole, toast])

  return (
    <div className="space-y-4 p-6" data-testid="role-templates-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Role Templates</h2>
        <p className="text-sm text-muted-foreground">
          Manage built-in and custom typed role templates used for delegation and constraint enforcement.
        </p>
      </div>

      {loadingError ? (
        <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
          <p className="text-sm text-destructive">{loadingError}</p>
          <Button className="mt-2" size="sm" variant="outline" onClick={() => void loadRoles(selectedRoleName)}>
            Retry
          </Button>
        </div>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-[320px_minmax(0,1fr)]">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Role list</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {loading ? <p className="text-sm text-muted-foreground">Loading role templates…</p> : null}
            {!loading && roles.length === 0 ? (
              <p className="text-sm text-muted-foreground">No roles found.</p>
            ) : null}
            {roles.map((role) => (
              <button
                key={role.name}
                type="button"
                data-testid={`role-item-${role.name}`}
                className={`w-full rounded-md border px-3 py-2 text-left text-sm transition-colors ${
                  selectedRoleName === role.name
                    ? "border-primary bg-primary/10"
                    : "hover:bg-muted"
                }`}
                onClick={() => onSelectRole(role.name)}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{role.name}</span>
                  <Badge variant={role.isBuiltin ? "secondary" : "outline"}>
                    {role.isBuiltin ? "Built-in" : "Custom"}
                  </Badge>
                </div>
                <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{role.description || "No description"}</p>
              </button>
            ))}
          </CardContent>
        </Card>

        <div className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Create custom role template</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <RoleFormFields
                prefix="create"
                form={createForm}
                disabled={creating}
                includeName
                onChange={updateCreateForm}
              />

              {createError ? <p className="text-sm text-destructive">{createError}</p> : null}

              <Button data-testid="create-role-submit" disabled={creating} onClick={() => void createRole()}>
                {creating ? "Creating…" : "Create role"}
              </Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Role editor</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {!selectedRole ? (
                <p className="text-sm text-muted-foreground">Select a role from the list to edit its constraints.</p>
              ) : (
                <>
                  <div className="flex items-center gap-2">
                    <span className="text-sm text-muted-foreground">Selected role:</span>
                    <span className="text-sm font-medium">{selectedRole.name}</span>
                    <Badge data-testid="role-selected-badge" variant={selectedRole.isBuiltin ? "secondary" : "outline"}>
                      {selectedRole.isBuiltin ? "Built-in" : "Custom"}
                    </Badge>
                  </div>

                  {selectedRole.isBuiltin ? (
                    <p data-testid="role-readonly-message" className="text-sm text-muted-foreground">
                      Built-in role templates are read-only and cannot be edited or deleted.
                    </p>
                  ) : null}

                  <RoleFormFields
                    prefix="edit"
                    form={editForm}
                    disabled={selectedRole.isBuiltin || saving || deleting}
                    includeName={false}
                    onChange={updateEditForm}
                  />

                  {editError ? <p className="text-sm text-destructive">{editError}</p> : null}

                  {!selectedRole.isBuiltin ? (
                    <div className="flex flex-wrap gap-2">
                      <Button data-testid="edit-role-submit" disabled={saving || deleting} onClick={() => void saveRole()}>
                        {saving ? "Saving…" : "Save role"}
                      </Button>
                      <Button
                        data-testid="edit-role-delete"
                        variant="destructive"
                        disabled={saving || deleting}
                        onClick={() => setDeleteDialogOpen(true)}
                      >
                        Delete role
                      </Button>
                    </div>
                  ) : null}
                </>
              )}
            </CardContent>
          </Card>
        </div>
      </div>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent data-testid="delete-role-confirm-dialog">
          <DialogHeader>
            <DialogTitle>Delete custom role template?</DialogTitle>
            <DialogDescription>
              This permanently deletes <strong>{selectedRole?.name || "this role"}</strong>. Built-in roles cannot be deleted.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              data-testid="delete-role-confirm-submit"
              variant="destructive"
              disabled={deleting}
              onClick={() => void deleteRole()}
            >
              {deleting ? "Deleting…" : "Delete"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
