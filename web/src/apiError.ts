// Keeps the HTTP status available to callers alongside the server error.
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}
