package repository

func storedPricingCurrency(currency string) string {
	if currency == "CNY" {
		return "CNY"
	}
	return "USD"
}
