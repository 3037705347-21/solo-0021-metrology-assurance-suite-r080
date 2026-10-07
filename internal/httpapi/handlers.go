package httpapi

import (
	"net/http"

	"example.com/solo-0021-metrology-assurance-suite/internal/contracts"
	"example.com/solo-0021-metrology-assurance-suite/internal/domain"
)

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) registerDevice(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.RegisterDeviceRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	device, err := s.service.RegisterDevice(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, contracts.DeviceResponse{Device: device})
}

func (s *Server) device(writer http.ResponseWriter, request *http.Request) {
	device, err := s.service.Device(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, contracts.DeviceResponse{Device: device})
}

func (s *Server) openCase(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.OpenCaseRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.OpenCase(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, view)
}

func (s *Server) caseView(writer http.ResponseWriter, request *http.Request) {
	view, err := s.service.Case(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) recordMeasurement(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.RecordMeasurementRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.RecordMeasurement(request.Context(), request.PathValue("id"), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, view)
}

func (s *Server) reopenCase(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.ReopenCaseRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.ReopenCase(request.Context(), request.PathValue("id"), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) sealCase(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.SealCaseRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.SealCase(request.Context(), request.PathValue("id"), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, view)
}

func (s *Server) events(writer http.ResponseWriter, request *http.Request) {
	view, err := s.service.Case(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, contracts.EventsResponse{Events: view.Events})
}

func (s *Server) registerBasis(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.RegisterBasisRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	basis, err := s.service.RegisterBasis(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, contracts.BasisResponse{Basis: basis})
}

func (s *Server) deviceBases(writer http.ResponseWriter, request *http.Request) {
	items, err := s.service.DeviceBases(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(writer, err)
		return
	}
	if items == nil {
		items = []domain.CalibrationBasis{}
	}
	writeJSON(writer, http.StatusOK, contracts.BasesResponse{Bases: items})
}

func (s *Server) createPlan(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.CreatePlanRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.CreatePlan(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, view)
}

func (s *Server) planView(writer http.ResponseWriter, request *http.Request) {
	view, err := s.service.Plan(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) listPlans(writer http.ResponseWriter, request *http.Request) {
	view, err := s.service.ListPlans(request.Context(), request.URL.Query().Get("status"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) arrangePlan(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.ArrangePlanRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.ArrangePlan(request.Context(), request.PathValue("id"), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, view)
}

func (s *Server) completePlan(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.CompletePlanRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.CompletePlan(request.Context(), request.PathValue("id"), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, view)
}

func (s *Server) reschedulePlan(writer http.ResponseWriter, request *http.Request) {
	input, err := decodeJSON[contracts.ReschedulePlanRequest](writer, request)
	if err != nil {
		writeError(writer, err)
		return
	}
	view, err := s.service.ReschedulePlan(request.Context(), request.PathValue("id"), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) planEvents(writer http.ResponseWriter, request *http.Request) {
	items, err := s.service.PlanEvents(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, contracts.EventsResponse{Events: items})
}
