package store

import "context"

type employeeActorKey struct{}

func WithEmployeeActor(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, employeeActorKey{}, id)
}
func employeeActor(ctx context.Context) string {
	if id, ok := ctx.Value(employeeActorKey{}).(string); ok && id != "" {
		return id
	}
	return "demo-clinician"
}
