package handler

func pricingCurrency(currency string) string {
	if currency == "CNY" {
		return "CNY"
	}
	return "USD"
}
