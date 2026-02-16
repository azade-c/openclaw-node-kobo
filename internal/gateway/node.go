package gateway

func DefaultRegistration() NodeRegistration {
	return NodeRegistration{
		Role: "node",
		Caps: []string{"canvas"},
		Commands: []string{
			"canvas.present",
			"canvas.hide",
			"canvas.navigate",
			"canvas.eval",
			"canvas.snapshot",
			"canvas.a2ui.push",
			"canvas.a2ui.pushJSONL",
			"canvas.a2ui.reset",
		},
	}
}
