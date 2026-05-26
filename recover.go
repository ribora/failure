package failure

func Catch(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil && err == nil {
			err = Panic(r)
		}
	}()
	return fn()
}
