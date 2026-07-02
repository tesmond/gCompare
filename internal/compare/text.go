package compare

func CompareText(leftText string, rightText string) FileComparisonResult {
	return buildRows(
		splitLines([]byte(leftText)),
		splitLines([]byte(rightText)),
		false,
		false,
		"Pasted left text",
		"Pasted right text",
		"",
	)
}
