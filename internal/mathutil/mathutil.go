package mathutil

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

func CeilDiv[T integer](a, b T) T {
	return (a + b - 1) / b
}
