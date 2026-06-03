package imagegen

import "testing"

func TestImageGenerationRequestMarshalOmitsNoneAndKeepsOrder(t *testing.T) {
	req := ImageGenerationRequest{
		Prompt:     "a cat",
		Background: ptr(ImageBackgroundAuto),
		Model:      "gpt-image-2",
		N:          nil,
		Quality:    ptr(ImageQualityAuto),
		Size:       ptr("auto"),
	}
	got, err := req.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"prompt":"a cat","background":"auto","model":"gpt-image-2","quality":"auto","size":"auto"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestImageGenerationRequestMarshalAllNoneOptional(t *testing.T) {
	req := ImageGenerationRequest{Prompt: "p", Model: "m"}
	got, err := req.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"prompt":"p","model":"m"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestImageEditRequestMarshalKeepsOrder(t *testing.T) {
	req := ImageEditRequest{
		Images:     []ImageURL{{ImageURL: "data:image/png;base64,x"}},
		Prompt:     "edit",
		Background: ptr(ImageBackgroundAuto),
		Model:      "gpt-image-2",
		Quality:    ptr(ImageQualityAuto),
		Size:       ptr("auto"),
	}
	got, err := req.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"images":[{"image_url":"data:image/png;base64,x"}],"prompt":"edit","background":"auto","model":"gpt-image-2","quality":"auto","size":"auto"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestParseArgsRejectsUnknownFields(t *testing.T) {
	if _, err := ParseArgs(`{"prompt":"p","action":"generate","extra":1}`); err == nil {
		t.Errorf("expected error for unknown field")
	}
	got, err := ParseArgs(`{"prompt":"p","action":"edit"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Prompt != "p" || got.Action != ImagegenActionEdit {
		t.Errorf("got %#v", got)
	}
}
