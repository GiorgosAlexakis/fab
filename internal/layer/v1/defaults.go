package v1

// SetDefaults_Layer fills in what a manifest may leave out.
//
// Origin defaults to local: a manifest read from a layer directory that does
// not say where it came from is one somebody wrote here.
func SetDefaults_Layer(obj *Layer) {
	if obj.APIVersion == "" {
		obj.APIVersion = APIVersion
	}
	if obj.Kind == "" {
		obj.Kind = LayerKind
	}
	if obj.Metadata.Origin == "" {
		obj.Metadata.Origin = OriginLocal
	}
}
