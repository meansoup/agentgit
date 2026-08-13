package terminal

import "testing"

func TestCtrlGQuestionMarkASCIIOpensHelp(t *testing.T) {
	s := &session{}

	s.handleInput([]byte{ctrlG, '?'})

	if !s.help {
		t.Fatal("help = false, want true")
	}
	if s.prefix {
		t.Fatal("prefix = true, want false")
	}
}

func TestCtrlGQuestionMarkCSIUOpensHelp(t *testing.T) {
	s := &session{}

	s.handleInput([]byte{ctrlG, esc, '[', '4', '7', ';', '2', 'u'})

	if !s.help {
		t.Fatal("help = false, want true")
	}
	if s.prefix {
		t.Fatal("prefix = true, want false")
	}
}

func TestCtrlGQuestionMarkModifyOtherKeysOpensHelp(t *testing.T) {
	s := &session{}

	s.handleInput([]byte{ctrlG, esc, '[', '2', '7', ';', '2', ';', '4', '7', '~'})

	if !s.help {
		t.Fatal("help = false, want true")
	}
	if s.prefix {
		t.Fatal("prefix = true, want false")
	}
}

func TestDecodePrefixSequenceWaitsForIncompleteCSI(t *testing.T) {
	if _, _, complete := decodePrefixSequence([]byte{esc, '[', '4', '7', ';', '2'}); complete {
		t.Fatal("incomplete CSI sequence was marked complete")
	}
}
