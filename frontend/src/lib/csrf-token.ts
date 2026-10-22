let csrfToken: string | null = null

export function getCsrfToken() {
  return csrfToken
}

export function setCsrfToken(token: string) {
  csrfToken = token
}

export function clearCsrfToken() {
  csrfToken = null
}
