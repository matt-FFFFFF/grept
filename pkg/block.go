package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/emirpasic/gods/queues/linkedlistqueue"
	"github.com/emirpasic/gods/sets/hashset"
	"github.com/google/uuid"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mcuadros/go-defaults"
	"github.com/zclconf/go-cty/cty"
	"strings"
)

type block interface {
	Id() string
	Name() string
	Type() string
	BlockType() string
	HclBlock() *hclBlock
	EvalContext() *hcl.EvalContext
	Values() map[string]cty.Value
	BaseValues() map[string]cty.Value
	forEachDefined() bool
	getDownstreams() []block
	setForEach(*forEach)
	getForEach() *forEach
}

func blockToString(f block) string {
	marshal, _ := json.Marshal(f)
	return string(marshal)
}

var publicAttributeNames = hashset.New("for_each", "rule_ids")

func decode(b block) error {
	defaults.SetDefaults(b)
	hb := b.HclBlock()
	evalContext := b.EvalContext()
	if hb.forEach != nil {
		evalContext = evalContext.NewChild()
		evalContext.Variables = map[string]cty.Value{
			"each": cty.ObjectVal(map[string]cty.Value{
				"key":   cty.StringVal(CtyValueToString(hb.key)),
				"value": hb.forEach.value,
			}),
		}
	}
	if decodeBase, ok := b.(DecodeBase); ok {
		err := decodeBase.Decode(hb, evalContext)
		if err != nil {
			return err
		}
	}
	diag := gohcl.DecodeBody(cleanBodyForDecode(hb), evalContext, b)
	if diag.HasErrors() {
		return diag
	}
	return nil
}

func cleanBodyForDecode(hb *hclBlock) *hclsyntax.Body {
	// Create a new hclsyntax.Body
	newBody := &hclsyntax.Body{
		Attributes: make(hclsyntax.Attributes),
		Blocks:     make([]*hclsyntax.Block, len(hb.Body.Blocks)),
	}

	// Iterate over the attributes of the original body
	for attrName, attr := range hb.Body.Attributes {

		if publicAttributeNames.Contains(attrName) {
			continue
		}
		newBody.Attributes[attrName] = attr
	}

	// Copy all blocks to the new body
	copy(newBody.Blocks, hb.Body.Blocks)

	return newBody
}

func LocalsValues(blocks []Local) cty.Value {
	if len(blocks) == 0 {
		return cty.EmptyObjectVal
	}
	res := map[string]cty.Value{}
	for _, b := range blocks {
		for _, v := range b.Values() {
			res[b.Name()] = v
		}
	}
	return cty.ObjectVal(res)
}

func Values[T block](blocks []T) cty.Value {
	if len(blocks) == 0 {
		return cty.EmptyObjectVal
	}
	res := map[string]cty.Value{}
	valuesMap := map[string]map[string]cty.Value{}

	for _, b := range blocks {
		values, exists := valuesMap[b.Type()]
		if !exists {
			values = map[string]cty.Value{}
			valuesMap[b.Type()] = values
		}
		blockVal := blockToCtyValue(b)
		forEach := b.getForEach()
		if forEach == nil {
			values[b.Name()] = blockVal
		} else {
			m, ok := values[b.Name()]
			if !ok {
				m = cty.MapValEmpty(cty.EmptyObject)
			}
			nm := m.AsValueMap()
			if nm == nil {
				nm = make(map[string]cty.Value)
			}
			nm[CtyValueToString(forEach.key)] = blockVal
			values[b.Name()] = cty.MapVal(nm)
		}
		valuesMap[b.Type()] = values
	}
	for t, m := range valuesMap {
		res[t] = cty.ObjectVal(m)
	}
	return cty.ObjectVal(res)
}

func blockToCtyValue(b block) cty.Value {
	blockValues := map[string]cty.Value{}
	baseCtyValues := b.BaseValues()
	ctyValues := b.Values()
	for k, v := range ctyValues {
		blockValues[k] = v
	}
	for k, v := range baseCtyValues {
		blockValues[k] = v
	}
	blockVal := cty.ObjectVal(blockValues)
	return blockVal
}

func concatLabels(labels []string) string {
	sb := strings.Builder{}
	for i, l := range labels {
		if l == "" {
			continue
		}
		sb.WriteString(l)
		if i != len(labels)-1 {
			sb.WriteString(".")
		}
	}
	return sb.String()
}

func blockAddress(b *hclBlock) string {
	sb := strings.Builder{}
	sb.WriteString(b.Block.Type)
	sb.WriteString(".")
	sb.WriteString(concatLabels(b.Block.Labels))
	if b.forEach != nil {
		sb.WriteString(fmt.Sprintf("[%s]", CtyValueToString(b.forEach.key)))
	}
	return sb.String()
}

type BaseBlock struct {
	c            *Config
	hb           *hclBlock
	name         string
	id           string
	blockAddress string
	forEach      *forEach
}

func newBaseBlock(c *Config, hb *hclBlock) *BaseBlock {
	bb := &BaseBlock{
		c:            c,
		hb:           hb,
		blockAddress: blockAddress(hb),
		name:         hb.Labels[1],
		id:           uuid.NewString(),
	}
	return bb
}

func (bb *BaseBlock) Id() string {
	return bb.id
}

func (bb *BaseBlock) Name() string {
	return bb.name
}

func (bb *BaseBlock) HclBlock() *hclBlock {
	if bb.hb == nil {
		return &hclBlock{
			Block: new(hclsyntax.Block),
		}
	}
	return bb.hb
}

func (bb *BaseBlock) BaseValues() map[string]cty.Value {
	return map[string]cty.Value{
		"id": cty.StringVal(bb.id),
	}
}

func (bb *BaseBlock) EvalContext() *hcl.EvalContext {
	if bb.c == nil {
		return new(hcl.EvalContext)
	}
	return bb.c.EvalContext()
}

func (bb *BaseBlock) Context() context.Context {
	if bb.c == nil {
		return context.TODO()
	}
	return bb.c.ctx
}

func (bb *BaseBlock) forEachDefined() bool {
	_, forEach := bb.HclBlock().Body.Attributes["for_each"]
	return forEach
}

func (bb *BaseBlock) getDownstreams() []block {
	var blocks []block
	children, _ := bb.c.dag.GetChildren(bb.blockAddress)
	for _, c := range children {
		blocks = append(blocks, c.(block))
	}
	return blocks
}

func (bb *BaseBlock) setForEach(each *forEach) {
	bb.forEach = each
}
func (bb *BaseBlock) getForEach() *forEach {
	return bb.forEach
}

func plan(c *Config, dag *Dag, q *linkedlistqueue.Queue, b block) error {
	self, _ := dag.GetVertex(blockAddress(b.HclBlock()))
	return planBlock(self.(block))
}

func tryEvalLocal(c *Config, dag *Dag, q *linkedlistqueue.Queue, b block) error {
	l, ok := b.(*LocalBlock)
	if !ok {
		return nil
	}
	value, diag := l.HclBlock().Body.Attributes["value"].Expr.Value(c.EvalContext())
	if !diag.HasErrors() {
		l.Value = value
	}
	return nil
}

func expandBlocks(c *Config, dag *Dag, q *linkedlistqueue.Queue, b block) error {
	attr, ok := b.HclBlock().Body.Attributes["for_each"]
	if !ok || b.getForEach() != nil {
		return nil
	}
	forEachValue, diag := attr.Expr.Value(c.EvalContext())
	if diag.HasErrors() {
		return diag
	}
	if !forEachValue.CanIterateElements() {
		return fmt.Errorf("invalid `for_each`, except set or map: %s", attr.Range().String())
	}
	address := blockAddress(b.HclBlock())
	upstreams, err := dag.GetAncestors(address)
	if err != nil {
		return err
	}
	downstreams, err := dag.GetChildren(address)
	if err != nil {
		return err
	}
	iterator := forEachValue.ElementIterator()
	for iterator.Next() {
		key, value := iterator.Element()
		newBlock := newHclBlock(b.HclBlock().Block, &forEach{key: key, value: value})
		nb, err := wrapBlock(c, newBlock)
		if err != nil {
			return err
		}
		expandedAddress := blockAddress(newBlock)
		err = dag.AddVertexByID(expandedAddress, nb)
		if err != nil {
			return err
		}
		for upstreamAddress := range upstreams {
			err := dag.addEdge(upstreamAddress, expandedAddress)
			if err != nil {
				return err
			}
		}
		for downstreamAddress := range downstreams {
			err := dag.addEdge(expandedAddress, downstreamAddress)
			if err != nil {
				return err
			}
		}
		q.Enqueue(nb)
	}
	return dag.DeleteVertex(address)
}

type DecodeBase interface {
	Decode(*hclBlock, *hcl.EvalContext) error
}
