package mapper

type Mapper[T any] func(T) T

func Identity[T any](t T) T {
	return t
}