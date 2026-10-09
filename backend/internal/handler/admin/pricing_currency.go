package admin

func pricingCurrencyOrUSD(currency string) string {
	if currency == "CNY" {
		return "CNY"
	}
	return "USD"
}
