package v1

// SetDefaults_Foundry fills in what a document may leave out.
//
// It runs on the way in and on the way out, so that a foundry.yaml written by
// hand without apiVersion and kind reads the same as one fab wrote.
func SetDefaults_Foundry(foundry *Foundry) {
	if foundry.APIVersion == "" {
		foundry.APIVersion = APIVersion
	}
	if foundry.Kind == "" {
		foundry.Kind = FoundryKind
	}
}
