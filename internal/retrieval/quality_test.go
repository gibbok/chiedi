package retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gibbok/chiedi/internal/embedding"
	"github.com/gibbok/chiedi/internal/indexer"
	"github.com/gibbok/chiedi/internal/store"
)

type qualityCase struct{ question, source, path string }

func TestFiftyCaseRetrievalQualityGate(t *testing.T) {
	pairs := [][2]string{
		{"automobile", "car"}, {"physician", "doctor"}, {"child", "kid"}, {"residence", "home"}, {"occupation", "job"},
		{"defect", "bug"}, {"begin", "start"}, {"finish", "complete"}, {"secure", "safe"}, {"assist", "help"},
		{"acquire", "obtain"}, {"require", "need"}, {"configure", "setup"}, {"remove", "delete"}, {"create", "generate"},
		{"change", "modify"}, {"folder", "directory"}, {"repository", "codebase"}, {"search", "locate"}, {"salary", "wage"},
		{"customer", "client"}, {"repair", "fix"}, {"incorrect", "wrong"}, {"permit", "allow"}, {"prohibit", "forbid"},
		{"select", "choose"}, {"display", "show"}, {"conceal", "hide"}, {"authenticate", "login"}, {"credential", "password"},
		{"execute", "run"}, {"stop", "halt"}, {"previous", "prior"}, {"next", "following"}, {"small", "tiny"},
		{"large", "huge"}, {"message", "notification"}, {"network", "internet"}, {"database", "storage"}, {"purchase", "buy"},
	}
	// Keep the same forty relationships and top-one assertions, but use explanatory
	// passages instead of bare labels from the removed production dictionary.
	descriptions := []string{
		"A car is a motor vehicle used to transport passengers on roads.",
		"A doctor is a qualified medical professional who diagnoses and treats illness.",
		"A kid is a young person who has not yet reached adulthood.",
		"A home is the place where a person lives and has their permanent address.",
		"A job is the paid work a person performs to earn a living.",
		"A bug is a flaw in software that makes it behave unexpectedly.",
		"Start marks the point at which an activity commences.",
		"Complete means to bring a task to its conclusion.",
		"Safe means protected against danger, harm, and unauthorized access.",
		"Help means to support someone in carrying out a task.",
		"Obtain means to gain possession of something.",
		"Need means something is necessary rather than optional.",
		"Setup establishes the settings and options needed to use a program.",
		"Delete means to erase an item so it is no longer present.",
		"Generate means to produce something new.",
		"Modify means to alter an existing item.",
		"A directory groups files together in a filesystem.",
		"A codebase contains the source files and version history of a software project.",
		"Locate means to find where a particular item is situated.",
		"A wage is the money paid to an employee for their work.",
		"A client is a person or organization buying goods or services from a business.",
		"Fix means to restore a broken item to working order.",
		"Wrong means a statement or result is not correct.",
		"Allow means to grant permission for an action.",
		"Forbid means to disallow an action under a rule.",
		"Choose means to pick one option from several alternatives.",
		"Show means to present information visibly on a screen.",
		"Hide means to keep an item out of sight.",
		"Login is the process of verifying a user identity before granting account access.",
		"A password is a secret string supplied as proof of identity to access an account.",
		"Run means to carry out the instructions in a computer program.",
		"Halt means to bring an ongoing activity to an end.",
		"Prior refers to something that came earlier in a sequence.",
		"Following refers to the item immediately after the current one in a sequence.",
		"Tiny describes something of very limited size.",
		"Huge describes something of great size or extent.",
		"A notification is a communication sent to inform someone about an event.",
		"The internet connects computers so they can exchange information.",
		"Storage holds organized data records in tables for later querying.",
		"Buy means to pay money in exchange for goods or services.",
	}
	// Context disambiguates polysemous words; the old dictionary treated every
	// occurrence as the same meaning. Every source must still rank first.
	contexts := []string{
		"road transport for passengers",
		"a professional treating illness",
		"a young person before adulthood",
		"the place a person lives",
		"paid employment on a job application",
		"a software flaw",
		"the commencement of an activity",
		"bringing a task to its conclusion",
		"protection from danger",
		"supporting someone with a task",
		"getting possession of something",
		"a mandatory necessity",
		"program settings and options",
		"erasing an item",
		"producing something new",
		"altering an existing item",
		"a group of files in a filesystem",
		"software source files and version history",
		"finding the position of an item",
		"pay for an employee",
		"a business buying goods or services",
		"restoring a broken item to working order",
		"a result that is not correct",
		"granting permission for an action",
		"disallowing an action under a rule",
		"picking one option among alternatives",
		"presenting information visibly on a screen",
		"keeping an item out of sight",
		"verifying identity during account access",
		"the secret string entered in an account access form",
		"carrying out computer program instructions",
		"ending an ongoing activity",
		"something earlier in a sequence",
		"the item immediately after the current one",
		"limited physical size",
		"great physical size",
		"a communication informing someone of an event",
		"computers exchanging information",
		"organized records in tables that can be queried",
		"paying money in exchange for goods",
	}
	cases := make([]qualityCase, 0, 50)
	for n, pair := range pairs {
		cases = append(cases, qualityCase{question: "What does " + pair[0] + " mean in the context of " + contexts[n] + "?", source: descriptions[n], path: fmt.Sprintf("semantic-%02d.txt", n)})
	}
	for n := 0; n < 10; n++ {
		reference := fmt.Sprintf("QA-ZX-%04d", 9000+n)
		cases = append(cases, qualityCase{question: reference, source: "Exact reference " + reference, path: fmt.Sprintf("identifier-%02d.txt", n)})
	}
	if len(cases) != 50 {
		t.Fatalf("quality dataset has %d cases, want 50", len(cases))
	}
	ctx := context.Background()
	root := t.TempDir()
	for _, test := range cases {
		if err := os.WriteFile(filepath.Join(root, test.path), []byte(test.source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "quality.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	embedder := embedding.E5{}
	stats, err := (indexer.Indexer{Store: s, Embedder: embedder}).Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.EmbeddingJobs != len(cases) {
		t.Fatalf("indexed %d embeddings, want %d", stats.EmbeddingJobs, len(cases))
	}
	retriever := Retriever{Store: s, Embedder: embedder}
	for n, test := range cases {
		t.Run(fmt.Sprintf("%02d_%s", n, test.path), func(t *testing.T) {
			results, err := retriever.Retrieve(ctx, test.question, "", 3)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) == 0 || results[0].Path != test.path {
				t.Fatalf("question %q: top results=%+v, want %s", test.question, results, test.path)
			}
		})
	}
}

// These document-level fixtures are independent of the single-word synonym
// table. They exercise grounding and distractors, not unrestricted semantics.
func TestDocumentQuestionRegression(t *testing.T) {
	cases := []qualityCase{
		{"Which port accepts incoming telemetry?", "The collector accepts telemetry on TCP port 4317. Health probes use port 8080.", "collector.txt"},
		{"How long are backups retained?", "Nightly backups are retained for thirty days. Restoration is rehearsed every quarter.", "backups.txt"},
		{"Who approves travel expenses?", "Travel expenses require approval from Elena before reimbursement. Receipts must be attached.", "expenses.txt"},
		{"When is the greenhouse irrigation scheduled?", "Greenhouse irrigation starts at 06:30 each morning. Rain sensors can postpone watering.", "garden.txt"},
		{"What happens after three failed login attempts?", "After three failed login attempts the account is locked for fifteen minutes.", "security.txt"},
	}
	ctx := context.Background()
	root := t.TempDir()
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(root, c.path), []byte(c.source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "documents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	e := embedding.E5{}
	if _, err := (indexer.Indexer{Store: s, Embedder: e}).Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		hits, err := (Retriever{Store: s, Embedder: e}).Retrieve(ctx, c.question, "", 1)
		if err != nil || len(hits) != 1 || hits[0].Path != c.path || hits[0].Text != c.source {
			t.Fatalf("%s: %+v %v", c.question, hits, err)
		}
	}
}

func TestCrossLanguageDocumentRetrieval(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docs := map[string]string{
		"leave.txt":   "Employees receive twenty days of paid vacation each year.",
		"backups.txt": "The database backup runs every night and is kept for thirty days.",
		"dentist.txt": "La visita dal dentista è fissata per martedì mattina.",
		"train.txt":   "Vlak do Prahy odjíždí v osm hodin ráno.",
	}
	for path, text := range docs {
		if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "multilingual.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.AddRoot(ctx, root); err != nil {
		t.Fatal(err)
	}
	e := embedding.E5{}
	if _, err = (indexer.Indexer{Store: s, Embedder: e}).Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ question, path string }{
		{"Quanti giorni di ferie spettano ai dipendenti?", "leave.txt"},
		{"Kolik dní dovolené mají zaměstnanci?", "leave.txt"},
		{"Wann wird die Datenbank gesichert?", "backups.txt"},
		{"数据库备份保留多久？", "backups.txt"},
		{"When is the dental appointment?", "dentist.txt"},
		{"A che ora parte il treno per Praga?", "train.txt"},
	} {
		hits, err := (Retriever{Store: s, Embedder: e}).Retrieve(ctx, tc.question, "", 1)
		if err != nil || len(hits) != 1 || hits[0].Path != tc.path || hits[0].Text != docs[tc.path] {
			t.Fatalf("%s: %+v %v", tc.question, hits, err)
		}
	}
}
