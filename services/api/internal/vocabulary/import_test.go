package vocabulary

import "testing"

func TestFourColumnImport(t *testing.T) {
	for _, text := range []string{"\ufeffword,phonetic,pos,chinese\nadapt,/əˈdæpt/,v.,适应\n\"look up\",/lʊk ʌp/,phr.,\"查询,查阅\"", "单词\t音标\t词性\t中文\nadapt\t/əˈdæpt/\tv.\t适应\nlook up\t/lʊk ʌp/\tphr.\t查阅", "adapt /əˈdæpt/ v. 适应\nlook up /lʊk ʌp/ phr. 查阅"} {
		p := Parse(text)
		if p.Valid() != nil || len(p.Entries) != 2 || p.Entries[1].Word != "look up" {
			t.Fatalf("valid four-field import failed: %+v", p)
		}
	}
	p := Parse("adapt /əˈdæpt/ v. 适应\nadapt /əˈdæpt/ v. 适应\nadapt /əˈdæpt/ n. 另一词性")
	if p.Valid() != nil || len(p.Entries) != 2 || p.Duplicates != 1 {
		t.Fatal("sense or duplicate handling failed")
	}
	capital := Parse("English /ˈɪŋɡlɪʃ/ n. 英语\nenglish /ˈɪŋɡlɪʃ/ n. 英语\nI /aɪ/ pron. 我")
	if capital.Valid() != nil || len(capital.Entries) != 2 || capital.Entries[0].Word != "English" || capital.Entries[1].Word != "I" || capital.Duplicates != 1 {
		t.Fatal("textbook capitalization lost or case duplicate retained")
	}
	for _, text := range []string{"adapt,missing,column", "adapt,,v.,适应", "=formula,/ipa/,v.,适应", "word,phonetic,pos,chinese", "adapt /ipa/ v. 适应\nbroken line", "adapt /ipa/ v. 适应\nadapt /other/ v. 适应"} {
		if Parse(text).Valid() == nil {
			t.Fatalf("invalid import accepted: %s", text)
		}
	}
}
