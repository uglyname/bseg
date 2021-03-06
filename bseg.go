package bseg

// Reference: http://aclweb.org/anthology//C/C12/C12-1127.pdf
// Reference: http://homepages.inf.ed.ac.uk/sgwater/papers/cognition-hdp.pdf [1]

// There are two parameters for DP-Seg, alpha and prob_seg.
// alpha is the Dirichlet base parameter:
//   Setting alpha small (e.g. 1) will lead to a smaller, denser multi-word
//   expression lexicon.
//   Setting alpha large (e.g. 1e6) will lead to a larger, sparser multi-word
//   expression lexicon.
//   Default: alpha = 20000 (should be on order of expected size of multi-word
//   expression lexicon).
// prob_seg is the probability of a SEG between any two tokens:
//   Setting prob_seg high will tend to produce shorter multi-word expressions.
//   Setting prob_seg low will tend to produce longer shorter multi-word
//   expressions.
//   Default prob_seg = 0.67.
//   This parameter is hard-coded in LogprobMWE.

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
)

const (
	NOSEG  uint8 = 1
	SEG    uint8 = 2
	FIXSEG uint8 = 3
)

var (
	logProbSeg   float64
	logProbNoSeg float64

	seg_prob         = flag.Float64("seg_prob", 0.5, "")
	ann_iters        = flag.Int("ann_iters", 100, "")
	iters            = flag.Int("iters", 100, "")
	alpha            = flag.Float64("alpha", 1, "")
	min_token_count  = flag.Int("min_token_count", 5, "")
	min_token_length = flag.Int("min_token_length", 1, "")
	print_log        = flag.Bool("print_log", true, "")
)

type BSeg struct {
	dict    map[string]int
	unigram map[string]int
}

func NewBSeg() *BSeg {
	s := new(BSeg)
	s.dict = make(map[string]int)
	s.unigram = make(map[string]int)
	rand.Seed(1234)
	return s
}

func (s *BSeg) DecrDict(word string) {
	v, found := s.dict[word]
	v--
	if found {
		if v > 0 {
			s.dict[word] = v
		} else {
			delete(s.dict, word)
		}
	}
}

func (s *BSeg) DumpDict(path string) {
	oFile, oErr := os.Create(path)
	if oErr != nil {
		log.Fatal(oErr)
	}
	defer oFile.Close()

	ts := make(Tokens, 0)
	for k, v := range s.dict {
		if v >= *min_token_count {
			ws := strings.Split(k, " ")
			if len(ws) >= *min_token_length {
				ts = append(ts, Token{Name: strings.Join(ws, ""), Count: v})
			}
		}
	}
	sort.Sort(ts)

	w := bufio.NewWriter(oFile)
	for i := 0; i < len(ts); i++ {
		fmt.Fprintf(w, "%s %d\n", ts[i].Name, ts[i].Count)
	}
	w.Flush()
}

func (s *BSeg) GetDict() Tokens {
	ts := make(Tokens, 0)
	for k, v := range s.dict {
		if v >= *min_token_count {
			ws := strings.Split(k, " ")
			if len(ws) >= *min_token_length {
				ts = append(ts, Token{Name: strings.Join(ws, ""), Count: v})
			}
		}
	}
	sort.Sort(ts)
	return ts
}

func (s *BSeg) FindInDict(word string) int {
	c, f := s.dict[word]
	if f {
		return c
	}
	return 0
}

func (s *BSeg) IncrDict(word string) {
	s.dict[word]++
}

func (s *BSeg) LogProbMWE(tokens []string, i1, i2 int) float64 {
	logProb := float64(0.0)
	N := len(s.dict) + len(s.unigram)
	for k := i1; k < i2; k++ {
		logProb += math.Log(float64(s.unigram[tokens[k]]+1.0) / float64(N))
	}
	logProb += math.Log(*seg_prob) + float64(i2-i1-1)*math.Log(1-*seg_prob)
	return logProb
}

func (s *BSeg) PrintDictStats() {
	kBuckets := 16
	numTypes := make([]int, kBuckets+1)
	numTokens := make([]int, kBuckets+1)

	for k, v := range s.dict {
		mwe := strings.Split(k, " ")
		l := len(mwe)
		l--
		if l > kBuckets {
			l = kBuckets
		}
		numTypes[l]++
		numTokens[l] += v
	}

	fmt.Printf("\tLength:\t")
	for i := 1; i < kBuckets; i++ {
		fmt.Printf("<%d>\t", i)
	}

	fmt.Printf("\n\tTypes:\t")
	for i := 1; i < kBuckets; i++ {
		fmt.Printf("%d\t", numTypes[i])
	}

	fmt.Printf("\n\tTokens:\t")
	for i := 1; i < kBuckets; i++ {
		fmt.Printf("%d\t", numTokens[i])
	}
	fmt.Printf("\n")
}

func (s *BSeg) ProcessText(tokens []string, segments []uint8) {
	for i := 0; i < len(tokens); i++ {
		s.unigram[tokens[i]]++
	}

	iEnd := 0
	for iEnd < len(tokens) {
		iStart := iEnd
		for iEnd < len(tokens)-1 && segments[iEnd] == NOSEG {
			iEnd++
		}
		iEnd++
		s.IncrDict(strings.Join(tokens[iStart:iEnd], " "))
	}

	for i := 0; i < (*ann_iters + *iters); i++ {
		temp := float64(i+1) / float64(*ann_iters)
		if temp > 1 {
			temp = 1
		}
		if *print_log {
			log.Printf("iter %d  Temp=%.2f", i, temp)
		}
		s.Sample(*alpha, temp, tokens, segments)
		if *print_log && i/10*10 == i {
			s.PrintDictStats()
		}
	}
}

func (s *BSeg) Sample(alpha, temperature float64, tokens []string, segments []uint8) {
	N := len(s.dict)
	invNPlusAlpha := 1.0 / (float64(N) + alpha)

	var mweL, mweR, mweLR string
	var i, iL, iR int
	var numL, numR, numLR int

	for i = 0; i < len(tokens)-1; i++ {
		if segments[i] == FIXSEG {
			continue
		}

		i1 := i + 1

		iL = i - 1
		for iL >= 0 && segments[iL] == NOSEG {
			iL--
		}
		iL++
		if i1-iL > 1 {
			mweL = strings.Join(tokens[iL:i1], " ")
		} else {
			mweL = tokens[i]
		}

		iR = i + 1
		for iR < len(tokens)-1 && segments[iR] == NOSEG {
			iR++
		}
		iR++
		if iR-i1 > 1 {
			mweR = strings.Join(tokens[i1:iR], " ")
		} else {
			mweR = tokens[i1]
		}
		mweLR = mweL + " " + mweR

		if segments[i] == SEG {
			numL = s.FindInDict(mweL)
			numR = s.FindInDict(mweR)
			numLR = s.FindInDict(mweLR)
			numL--
			numR--
		} else {
			numL = s.FindInDict(mweL)
			numR = s.FindInDict(mweR)
			numLR = s.FindInDict(mweLR)
			numLR--
		}

		var sumProb float64
		logProbL := s.LogProbMWE(tokens, iL, i1)
		logProbR := s.LogProbMWE(tokens, i1, iR)
		logProbLR := logProbL + logProbR

		prob0 := (float64(numLR) + alpha*math.Exp(logProbLR)) * invNPlusAlpha
		prob1L := (float64(numL) + alpha*math.Exp(logProbL)) * invNPlusAlpha
		prob1R := (float64(numR) + alpha*math.Exp(logProbR)) * invNPlusAlpha
		prob1 := prob1L * prob1R

		if temperature < 0.999 {
			sumProb = prob0 + prob1
			prob0 /= sumProb
			prob1 /= sumProb
			prob0 = math.Pow(prob0, temperature)
			prob1 = math.Pow(prob1, temperature)
		}

		sumProb = prob0 + prob1
		prob0 /= sumProb
		insertSeg := rand.Float64() > prob0

		if segments[i] == NOSEG && insertSeg {
			segments[i] = SEG
			s.DecrDict(mweLR)
			s.IncrDict(mweL)
			s.IncrDict(mweR)
		} else if segments[i] == SEG && !insertSeg {
			segments[i] = NOSEG
			s.DecrDict(mweL)
			s.DecrDict(mweR)
			s.IncrDict(mweLR)
		}
	}
}
