//go:build !darwin

package dream

// platformSandbox returns the backend for the current platform. Only macOS
// Seatbelt has been verified so far; every other platform reports Dreaming as
// unsupported rather than running an unrestricted runner. See
// docs/05-validation-and-delivery.md section 3.
func platformSandbox() Sandbox { return Unavailable{} }
