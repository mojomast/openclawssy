import { FormEvent, useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  clearAuthTokenGateError,
  getAuthTokenGateState,
  resolveBearerToken,
  submitAuthToken,
  subscribeAuthTokenGate,
} from '@/lib/api'

export function AuthTokenGate() {
  const [gateState, setGateState] = useState(getAuthTokenGateState)
  const [token, setToken] = useState('')

  useEffect(() => {
    return subscribeAuthTokenGate(setGateState)
  }, [])

  useEffect(() => {
    void resolveBearerToken().catch(() => {
      // Request will remain pending until the user submits a token via the gate.
    })
  }, [])

  if (!gateState.open) {
    return null
  }

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (submitAuthToken(token)) {
      setToken('')
    }
  }

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-background/95 p-6">
      <div className="w-full max-w-md rounded-lg border bg-card p-6 shadow-xl">
        <h2 className="text-xl font-semibold tracking-tight">Dashboard access token required</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Enter the bearer token to continue. Pending dashboard requests will resume automatically once saved.
        </p>

        <form className="mt-5 space-y-3" onSubmit={handleSubmit}>
          <label className="space-y-2 text-sm" htmlFor="dashboard-token-input">
            <span className="font-medium">Bearer token</span>
            <Input
              id="dashboard-token-input"
              aria-label="Dashboard bearer token"
              type="password"
              autoComplete="off"
              autoFocus
              placeholder="Enter dashboard bearer token"
              value={token}
              onChange={(event) => {
                if (gateState.errorMessage) {
                  clearAuthTokenGateError()
                }
                setToken(event.target.value)
              }}
            />
          </label>

          {gateState.errorMessage && (
            <p className="text-sm text-destructive" role="alert">
              {gateState.errorMessage}
            </p>
          )}

          <Button className="w-full" type="submit">
            Save token and continue
          </Button>
        </form>
      </div>
    </div>
  )
}
