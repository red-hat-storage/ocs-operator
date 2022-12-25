package types

type DiffReason uint16

const (
	NoDifference = DiffReason(iota)
	DifferentExpr
	AlertOnlyWithMe
	AlertOnlyWithThem
	DifferenceInNoOfRules // number of rules differ

	Same = NoDifference
)

func (diffR DiffReason) String() (diffRStr string) {
	var diffReasonString = []string{
		NoDifference:          "no difference",
		DifferentExpr:         "difference in expression",
		AlertOnlyWithMe:       "the alert is present only with upstream",
		AlertOnlyWithThem:     "the alert is present only with downstream",
		DifferenceInNoOfRules: "no: of rules with the alert name differs",
	}
	switch diffR {
	case NoDifference, DifferentExpr, AlertOnlyWithMe, AlertOnlyWithThem, DifferenceInNoOfRules:
		diffRStr = diffReasonString[diffR]
	default:
		diffRStr = "custom difference"
	}
	return
}

type DiffReasonSubUnit struct {
	DiffReason  DiffReason
	DiffMessage string
	Rule1       []Rule
	Rule2       []Rule
}

func NewDiffReasonSubUnit(diffReason DiffReason, diffMessage string,
	rule1 []Rule, rule2 []Rule) DiffReasonSubUnit {
	retDiffSubUnit := DiffReasonSubUnit{
		DiffReason:  diffReason,
		DiffMessage: diffMessage,
	}
	retDiffSubUnit.Rule1 = append(retDiffSubUnit.Rule1, rule1...)
	retDiffSubUnit.Rule2 = append(retDiffSubUnit.Rule2, rule2...)
	return retDiffSubUnit
}

type PrometheusRuleDiffUnit struct {
	Alert       string
	DiffReasons []DiffReasonSubUnit
}
