package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tessro/fab/internal/daemon"
)

// handleUserQuestionRequest handles a user question request from the hook command.
// This blocks until a TUI client responds via question.respond.
func (s *Supervisor) handleUserQuestionRequest(_ context.Context, req *daemon.Request) *daemon.Response {
	var questionReq daemon.UserQuestionRequestPayload
	if err := unmarshalPayload(req.Payload, &questionReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if len(questionReq.Questions) == 0 {
		return errorResponse(req, "questions are required")
	}

	// Find the project for this agent
	var project string
	if questionReq.AgentID != "" {
		if a, err := s.agents.Get(questionReq.AgentID); err == nil {
			project = a.Info().Project
		}
	}

	slog.Info("user question request received",
		"agent", questionReq.AgentID,
		"project", project,
		"question_count", len(questionReq.Questions),
	)

	// Create the user question
	userQuestion := &daemon.UserQuestion{
		AgentID:     questionReq.AgentID,
		Project:     project,
		Questions:   questionReq.Questions,
		RequestedAt: time.Now(),
	}

	// Add to the question manager and get the response channel
	id, respCh := s.questions.Add(userQuestion)
	userQuestion.ID = id

	// Broadcast the user question to attached TUI clients
	s.broadcastUserQuestion(userQuestion)

	// Block waiting for a response from the TUI
	resp := <-respCh
	if resp == nil {
		slog.Warn("user question request timed out",
			"id", id,
			"agent", questionReq.AgentID,
		)
		// Channel was closed without a response (timeout or cancellation)
		return errorResponse(req, "user question cancelled or timed out")
	}

	slog.Info("user question response sent",
		"id", id,
		"agent", questionReq.AgentID,
		"answers", resp.Answers,
	)

	return successResponse(req, resp)
}

// handleUserQuestionRespond handles a user question response from the TUI.
func (s *Supervisor) handleUserQuestionRespond(_ context.Context, req *daemon.Request) *daemon.Response {
	var respPayload daemon.UserQuestionRespondPayload
	if err := unmarshalPayload(req.Payload, &respPayload); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if respPayload.ID == "" {
		return errorResponse(req, "question request ID required")
	}

	// Get the original question for logging
	origQuestion := s.questions.Get(respPayload.ID)
	if origQuestion != nil {
		slog.Info("user question response from TUI",
			"id", respPayload.ID,
			"agent", origQuestion.AgentID,
			"answers", respPayload.Answers,
		)
	} else {
		slog.Info("user question response from TUI",
			"id", respPayload.ID,
			"answers", respPayload.Answers,
		)
	}

	resp := &daemon.UserQuestionResponse{
		ID:      respPayload.ID,
		Answers: respPayload.Answers,
	}

	if err := s.questions.Respond(respPayload.ID, resp); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to respond: %v", err))
	}

	return successResponse(req, nil)
}
