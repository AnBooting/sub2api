package repository

func nullablePricingSnapshot(snapshot []byte) any {
	if len(snapshot) == 0 {
		return nil
	}
	return string(snapshot)
}
