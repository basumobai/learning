package platform

import "testing"

func TestGuangdongSectionsAndEnglishCurriculum(t *testing.T) {
	for _, subject := range []string{"chinese", "math", "english", "physics", "chemistry", "biology"} {
		p := GuangdongExam(subject)
		if p.PolicyYear != 2026 || len(p.Sections) == 0 || p.Minutes <= 0 || len(p.Sources) == 0 {
			t.Fatal(subject, p)
		}
		for _, section := range p.Sections {
			for _, kind := range section.Kinds {
				if !ValidType(subject, kind) {
					t.Fatal("unusable exam type", subject, kind)
				}
			}
		}
	}
	for _, kind := range []string{"modern_i", "modern_ii"} {
		if !ValidType("chinese", kind) {
			t.Fatal(kind)
		}
	}
	if !ValidType("chemistry", "industrial_process") || !ValidType("biology", "experimental_design") {
		t.Fatal("science subtypes remain too broad")
	}
	// Existing questions remain accessible after introducing finer classifications.
	if !ValidType("chinese", "modern_reading") || !ValidType("biology", "non_choice") || !ValidType("english", "speaking") {
		t.Fatal("legacy type removed")
	}
	p := Curriculum("english")
	if !p.EditionLocked || p.Edition != EnglishEdition || len(p.Volumes) != 8 {
		t.Fatal(p)
	}
	if ValidateCurriculum("english", EnglishEdition, "选择性必修第一册") != nil {
		t.Fatal("current BNU textbook rejected")
	}
	for _, input := range [][3]string{{"english", "人教版2019", "必修第一册"}, {"english", EnglishEdition, "Unit 1"}, {"unknown", "人教版", "必修第一册"}} {
		if ValidateCurriculum(input[0], input[1], input[2]) == nil {
			t.Fatal("invalid curriculum accepted", input)
		}
	}
	if ValidateCurriculum("physics", "粤教版2019", "选择性必修 第二册") != nil {
		t.Fatal("other publisher's actual volume spelling rejected")
	}
}
