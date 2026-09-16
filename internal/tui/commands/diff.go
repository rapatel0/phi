package commands

// DiffCommands owns the /diff slash command.
type DiffCommands struct {
	Open func(args []string)
}

// Register wires /diff into r.
func (d *DiffCommands) Register(r *CommandRegistry) {
	if d == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "diff",
		Description: "Review git diff — /diff, /diff staged, /diff HEAD",
		Slash:       true,
		Insert:      "/diff ",
		Run: func(ctx CommandContext) error {
			if ctx.OpenDiff != nil {
				ctx.OpenDiff(ctx.Args)
			}
			return nil
		},
	})
}
