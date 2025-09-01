package provider

func ViewEquivalent(yaml1, yaml2 string) (bool, error) {
	//TODO move to a util function
	return SyntheticChecksEquivalent(yaml1, yaml2)
}
