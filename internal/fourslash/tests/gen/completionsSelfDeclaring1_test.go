package fourslash_test

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/fourslash"
	"github.com/microsoft/typescript-go/internal/ls"
	"github.com/microsoft/typescript-go/internal/lsp/lsproto"
	"github.com/microsoft/typescript-go/internal/testutil"
)

func TestCompletionsSelfDeclaring1(t *testing.T) {
	t.Parallel()

	defer testutil.RecoverAndFail(t, "Panic on fourslash test")
	const content = `interface Test {
  keyPath?: string;
  autoIncrement?: boolean;
}

function test<T extends Record<string, Test>>(opt: T) { }

test({
  a: {
    keyPath: '',
    a/**/
  }
})`
	f := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	f.VerifyCompletions(t, "", &fourslash.CompletionsExpectedList{
		IsIncomplete: false,
		ItemDefaults: &fourslash.CompletionsExpectedItemDefaults{
			CommitCharacters: &defaultCommitCharacters,
			EditRange:        ignored,
		},
		Items: &fourslash.CompletionsExpectedItems{
			Exact: []fourslash.CompletionsExpectedItem{&lsproto.CompletionItem{SortText: ptrTo(string(ls.SortTextOptionalMember)), Label: "autoIncrement?", InsertText: ptrTo("autoIncrement"), FilterText: ptrTo("autoIncrement")}},
		},
	})
}
