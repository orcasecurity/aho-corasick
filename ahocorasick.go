package aho_corasick

import (
	"strings"
	"sync"
	"unicode"
)

type findIter struct {
	fsm                 imp
	prestate            *prefilterState
	haystack            []byte
	pos                 int
	matchOnlyWholeWords bool
}

// Iter is an iterator over matches found on the current haystack
// it gives the user more granular control. You can chose how many and what kind of matches you need.
type Iter interface {
	Next() *Match
}

// Next gives a pointer to the next match yielded by the iterator or nil, if there is none
func (f *findIter) Next() *Match {
	if f.pos > len(f.haystack) {
		return nil
	}

	result := f.fsm.FindAtNoState(f.prestate, f.haystack, f.pos)

	if result == nil {
		return nil
	}

	f.pos = result.end - result.len + 1

	if f.matchOnlyWholeWords {
		if result.Start()-1 >= 0 && (unicode.IsLetter(rune(f.haystack[result.Start()-1])) || unicode.IsDigit(rune(f.haystack[result.Start()-1]))) {
			return f.Next()
		}
		if result.end < len(f.haystack) && (unicode.IsLetter(rune(f.haystack[result.end])) || unicode.IsDigit(rune(f.haystack[result.end]))) {
			return f.Next()
		}
	}

	return result
}

type overlappingIter struct {
	fsm                 imp
	prestate            *prefilterState
	haystack            []byte
	pos                 int
	stateID             stateID
	matchIndex          int
	matchOnlyWholeWords bool
}

func (f *overlappingIter) Next() *Match {
	if f.pos > len(f.haystack) {
		return nil
	}

	result := f.fsm.OverlappingFindAt(f.prestate, f.haystack, f.pos, &f.stateID, &f.matchIndex)

	if result == nil {
		return nil
	}

	f.pos = result.End()

	if f.matchOnlyWholeWords {
		if result.Start()-1 >= 0 && (unicode.IsLetter(rune(f.haystack[result.Start()-1])) || unicode.IsDigit(rune(f.haystack[result.Start()-1]))) {
			return f.Next()
		}
		if result.end < len(f.haystack) && (unicode.IsLetter(rune(f.haystack[result.end])) || unicode.IsDigit(rune(f.haystack[result.end]))) {
			return f.Next()
		}
	}

	return result
}

func newOverlappingIter(ac AhoCorasick, haystack []byte) overlappingIter {
	prestate := prefilterState{
		skips:       0,
		skipped:     0,
		maxMatchLen: ac.i.MaxPatternLen(),
		inert:       false,
		lastScanAt:  0,
	}
	return overlappingIter{
		fsm:                 ac.i,
		prestate:            &prestate,
		haystack:            haystack,
		pos:                 0,
		stateID:             ac.i.StartState(),
		matchIndex:          0,
		matchOnlyWholeWords: ac.matchOnlyWholeWords,
	}
}

// make sure the AhoCorasick data structure implements the Finder interface
var _ Finder = (*AhoCorasick)(nil)

// AhoCorasick is the main data structure that does most of the work
type AhoCorasick struct {
	i                   imp
	matchKind           matchKind
	matchOnlyWholeWords bool
}

func (ac AhoCorasick) PatternCount() int {
	return ac.i.PatternCount()
}

// Iter gives an iterator over the built patterns
func (ac AhoCorasick) Iter(haystack string) Iter {
	return ac.IterByte([]byte(haystack))
}

// IterByte gives an iterator over the built patterns
func (ac AhoCorasick) IterByte(haystack []byte) Iter {
	prestate := &prefilterState{
		skips:       0,
		skipped:     0,
		maxMatchLen: ac.i.MaxPatternLen(),
		inert:       false,
		lastScanAt:  0,
	}

	return &findIter{
		fsm:                 ac.i,
		prestate:            prestate,
		haystack:            haystack,
		pos:                 0,
		matchOnlyWholeWords: ac.matchOnlyWholeWords,
	}
}

// Iter gives an iterator over the built patterns with overlapping matches
func (ac AhoCorasick) IterOverlapping(haystack string) Iter {
	return ac.IterOverlappingByte([]byte(haystack))
}

// IterOverlappingByte gives an iterator over the built patterns with overlapping matches
func (ac AhoCorasick) IterOverlappingByte(haystack []byte) Iter {
	if ac.matchKind != StandardMatch {
		panic("only StandardMatch allowed for overlapping matches")
	}
	i := newOverlappingIter(ac, haystack)
	return &i
}

var pool = sync.Pool{
	New: func() interface{} {
		return strings.Builder{}
	},
}

type Replacer struct {
	finder Finder
}

func NewReplacer(finder Finder) Replacer {
	return Replacer{finder: finder}
}

// ReplaceAllFunc replaces the matches found in the haystack according to the user provided function
// it gives fine grained control over what is replaced.
// A user can chose to stop the replacing process early by returning false in the lambda
// In that case, everything from that point will be kept as the original haystack
func (r Replacer) ReplaceAllFunc(haystack string, f func(match Match) (string, bool)) string {
	matches := r.finder.FindAll(haystack)

	if len(matches) == 0 {
		return haystack
	}

	replaceWith := make([]string, 0)

	for _, match := range matches {
		rw, ok := f(match)
		if !ok {
			break
		}
		replaceWith = append(replaceWith, rw)
	}

	str := pool.Get().(strings.Builder)

	defer func() {
		str.Reset()
		pool.Put(str)
	}()

	start := 0

	for i, match := range matches {
		if i >= len(replaceWith) {
			str.WriteString(haystack[start:])
			return str.String()
		}
		str.WriteString(haystack[start:match.Start()])
		str.WriteString(replaceWith[i])
		start = match.Start() + match.len
	}

	if start-1 < len(haystack) {
		str.WriteString(haystack[start:])
	}

	return str.String()
}

// ReplaceAll replaces the matches found in the haystack according to the user provided slice `replaceWith`
// It panics, if `replaceWith` has length different from the patterns that it was built with
func (r Replacer) ReplaceAll(haystack string, replaceWith []string) string {
	if len(replaceWith) != r.finder.PatternCount() {
		panic("replaceWith needs to have the same length as the pattern count")
	}

	return r.ReplaceAllFunc(haystack, func(match Match) (string, bool) {
		return replaceWith[match.pattern], true
	})
}

type Finder interface {
	FindAll(haystack string) []Match
	PatternCount() int
}

// MatchState stores reusable scratch space for zero-allocation matching.
type MatchState struct {
	prestate prefilterState
	match    Match
}

func (s *MatchState) reset(maxPatternLen int) {
	s.prestate.skips = 0
	s.prestate.skipped = 0
	s.prestate.maxMatchLen = maxPatternLen
	s.prestate.inert = false
	s.prestate.lastScanAt = 0
}

// NewMatchState creates reusable state for FindAllByteToWithState.
func (ac AhoCorasick) NewMatchState() MatchState {
	state := MatchState{}
	state.reset(ac.i.MaxPatternLen())
	return state
}

func isWordByte(b byte) bool {
	return unicode.IsLetter(rune(b)) || unicode.IsDigit(rune(b))
}

// FindAll returns the matches found in the haystack
func (ac AhoCorasick) FindAll(haystack string) []Match {
	return ac.FindAllByteTo([]byte(haystack), nil)
}

// FindAllByte returns the matches found in the haystack bytes.
func (ac AhoCorasick) FindAllByte(haystack []byte) []Match {
	return ac.FindAllByteTo(haystack, nil)
}

// FindAllTo appends matches into dst and returns the resulting slice.
// Note: converting a string to []byte allocates once; for zero-allocation matching use FindAllByteTo.
func (ac AhoCorasick) FindAllTo(haystack string, dst []Match) []Match {
	return ac.FindAllByteTo([]byte(haystack), dst)
}

// FindAllByteTo appends matches into dst and returns the resulting slice.
// If dst has enough capacity, this path does not allocate.
func (ac AhoCorasick) FindAllByteTo(haystack []byte, dst []Match) []Match {
	state := ac.NewMatchState()
	return ac.findAllByteTo(haystack, dst, &state.prestate, &state.match)
}

// FindAllByteToWithState appends matches into dst and returns the resulting slice.
// Reusing both state and dst enables a zero-allocation matching path.
func (ac AhoCorasick) FindAllByteToWithState(haystack []byte, dst []Match, state *MatchState) []Match {
	if state == nil {
		panic("state cannot be nil")
	}
	state.reset(ac.i.MaxPatternLen())
	return ac.findAllByteTo(haystack, dst, &state.prestate, &state.match)
}

func (ac AhoCorasick) findAllByteTo(haystack []byte, dst []Match, prestate *prefilterState, match *Match) []Match {
	pos := 0

	for pos <= len(haystack) {
		if !ac.i.FindAtNoStateInto(prestate, haystack, pos, match) {
			break
		}

		start := match.Start()
		pos = start + 1

		if ac.matchOnlyWholeWords {
			if start-1 >= 0 && isWordByte(haystack[start-1]) {
				continue
			}
			if match.end < len(haystack) && isWordByte(haystack[match.end]) {
				continue
			}
		}

		dst = append(dst, *match)
	}
	return dst
}

// ContainsByte reports whether haystack contains at least one match.
func (ac AhoCorasick) ContainsByte(haystack []byte) bool {
	state := ac.NewMatchState()
	return ac.ContainsByteWithState(haystack, &state)
}

// ContainsByteWithState reports whether haystack contains at least one match.
// Reusing state enables a zero-allocation contains check.
func (ac AhoCorasick) ContainsByteWithState(haystack []byte, state *MatchState) bool {
	if state == nil {
		panic("state cannot be nil")
	}
	state.reset(ac.i.MaxPatternLen())
	return ac.i.FindAtNoStateInto(&state.prestate, haystack, 0, &state.match)
}

// AhoCorasickBuilder defines a set of options applied before the patterns are built
type AhoCorasickBuilder struct {
	dfaBuilder          *iDFABuilder
	nfaBuilder          *iNFABuilder
	dfa                 bool
	matchOnlyWholeWords bool
}

// Opts defines a set of options applied before the patterns are built
// MatchOnlyWholeWords does filtering after matching with MatchKind
// this could lead to situations where, in this case, nothing is matched
//
//	    trieBuilder := NewAhoCorasickBuilder(Opts{
//		     MatchOnlyWholeWords: true,
//		     MatchKind:           LeftMostLongestMatch,
//		     DFA:                 false,
//	    })
//
//			trie := trieBuilder.Build([]string{"testing", "testing 123"})
//			result := trie.FindAll("testing 12345")
//		 len(result) == 0
//
// this is due to the fact LeftMostLongestMatch is the matching strategy
// "testing 123" is found but then is filtered out by MatchOnlyWholeWords
// use MatchOnlyWholeWords with caution
type Opts struct {
	AsciiCaseInsensitive bool
	MatchOnlyWholeWords  bool
	MatchKind            matchKind
	DFA                  bool
}

// NewAhoCorasickBuilder creates a new AhoCorasickBuilder based on Opts
func NewAhoCorasickBuilder(o Opts) AhoCorasickBuilder {
	return AhoCorasickBuilder{
		dfaBuilder:          newDFABuilder(),
		nfaBuilder:          newNFABuilder(o.MatchKind, o.AsciiCaseInsensitive),
		dfa:                 o.DFA,
		matchOnlyWholeWords: o.MatchOnlyWholeWords,
	}
}

// Build builds a (non)deterministic finite automata from the user provided patterns
func (a *AhoCorasickBuilder) Build(patterns []string) AhoCorasick {
	bytePatterns := make([][]byte, len(patterns))
	for pati, pat := range patterns {
		bytePatterns[pati] = []byte(pat)
	}

	return a.BuildByte(bytePatterns)
}

// BuildByte builds a (non)deterministic finite automata from the user provided patterns
func (a *AhoCorasickBuilder) BuildByte(patterns [][]byte) AhoCorasick {
	nfa := a.nfaBuilder.build(patterns)
	match_kind := nfa.matchKind

	if a.dfa {
		dfa := a.dfaBuilder.build(nfa)
		return AhoCorasick{dfa, match_kind, a.matchOnlyWholeWords}
	}

	return AhoCorasick{nfa, match_kind, a.matchOnlyWholeWords}
}

type imp interface {
	MatchKind() *matchKind
	StartState() stateID
	MaxPatternLen() int
	PatternCount() int
	Prefilter() prefilter
	UsePrefilter() bool
	OverlappingFindAt(prestate *prefilterState, haystack []byte, at int, state_id *stateID, match_index *int) *Match
	EarliestFindAt(prestate *prefilterState, haystack []byte, at int, state_id *stateID) *Match
	FindAtNoState(prestate *prefilterState, haystack []byte, at int) *Match
	FindAtNoStateInto(prestate *prefilterState, haystack []byte, at int, dst *Match) bool
}

type matchKind int

const (
	// Use standard match semantics, which support overlapping matches. When
	// used with non-overlapping matches, matches are reported as they are seen.
	StandardMatch matchKind = iota
	// Use leftmost-first match semantics, which reports leftmost matches.
	// When there are multiple possible leftmost matches, the match
	// corresponding to the pattern that appeared earlier when constructing
	// the automaton is reported.
	// This does **not** support overlapping matches or stream searching
	LeftMostFirstMatch
	// Use leftmost-longest match semantics, which reports leftmost matches.
	// When there are multiple possible leftmost matches, the longest match is chosen.
	LeftMostLongestMatch
)

func (m matchKind) supportsOverlapping() bool {
	return m.isStandard()
}

func (m matchKind) supportsStream() bool {
	return m.isStandard()
}

func (m matchKind) isStandard() bool {
	return m == StandardMatch
}

func (m matchKind) isLeftmost() bool {
	return m == LeftMostFirstMatch || m == LeftMostLongestMatch
}

func (m matchKind) isLeftmostFirst() bool {
	return m == LeftMostFirstMatch
}

// A representation of a match reported by an Aho-Corasick automaton.
//
// A match has two essential pieces of information: the identifier of the
// pattern that matched, along with the start and end offsets of the match
// in the haystack.
type Match struct {
	pattern int
	len     int
	end     int
}

// Pattern returns the index of the pattern in the slice of the patterns provided by the user that
// was matched
func (m *Match) Pattern() int {
	return m.pattern
}

// End gives the index of the last character of this match inside the haystack
func (m *Match) End() int {
	return m.end
}

// Start gives the index of the first character of this match inside the haystack
func (m *Match) Start() int {
	return m.end - m.len
}

type stateID uint

const (
	failedStateID stateID = 0
	deadStateID   stateID = 1
)
