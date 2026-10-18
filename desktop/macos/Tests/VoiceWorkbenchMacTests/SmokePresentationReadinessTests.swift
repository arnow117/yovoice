import XCTest
@testable import VoiceWorkbenchMac

final class SmokePresentationReadinessTests: XCTestCase {
    private func readiness(
        windowVisible: Bool = true,
        windowMiniaturized: Bool = false,
        webViewAttached: Bool = true,
        width: Double = 1280,
        height: Double = 840,
        documentVisibility: String = "visible",
        applicationActive: Bool = true,
        windowOcclusionVisible: Bool = true,
    ) -> SmokePresentationReadiness {
        SmokePresentationReadiness(
            windowVisible: windowVisible,
            windowMiniaturized: windowMiniaturized,
            webViewAttached: webViewAttached,
            webViewWidth: width,
            webViewHeight: height,
            documentVisibility: documentVisibility,
            applicationActive: applicationActive,
            windowOcclusionVisible: windowOcclusionVisible,
        )
    }

    func testVisibleAttachedPagePassesPreflight() {
        let presentation = readiness()

        XCTAssertNil(presentation.failureReason)
        XCTAssertTrue(presentation.applicationActive)
        XCTAssertTrue(presentation.windowOcclusionVisible)
    }

    func testPreflightRejectsNativePresentationFailuresBeforeSmokeScript() {
        XCTAssertEqual(readiness(windowVisible: false).failureReason, "window_not_visible")
        XCTAssertEqual(readiness(windowMiniaturized: true).failureReason, "window_minimized")
        XCTAssertEqual(readiness(webViewAttached: false).failureReason, "webview_not_attached")
        XCTAssertEqual(readiness(width: 0).failureReason, "webview_zero_size")
    }

    func testHiddenDocumentStillFailsWhenTheApplicationIsActive() {
        let presentation = readiness(
            documentVisibility: "hidden",
            applicationActive: true,
            windowOcclusionVisible: true,
        )

        XCTAssertEqual(presentation.failureReason, "document_not_visible")
    }

    func testDocumentVisibilityRequestTimesOutWithoutCallback() async {
        let request = SmokeDocumentVisibilityRequest()
        let visibility = await request.wait(timeoutNanoseconds: 10_000_000) { _ in }

        XCTAssertNil(visibility)
        XCTAssertTrue(request.completed)
    }

    func testDocumentVisibilityRequestIgnoresLateVisibleCallback() async {
        let request = SmokeDocumentVisibilityRequest()
        var callback: ((String?) -> Void)?
        let visibility = await request.wait(timeoutNanoseconds: 10_000_000) { completion in
            callback = completion
        }

        XCTAssertNil(visibility)
        XCTAssertTrue(request.completed)
        callback?("visible")
        XCTAssertTrue(request.completed)
    }

    func testDocumentVisibilityRequestRejectsVisibleCallbackAfterDeadlineBeforeTimerRuns() async {
        var now: UInt64 = 10
        let request = SmokeDocumentVisibilityRequest(clock: { now })
        let visibility = await request.wait(timeoutNanoseconds: 10) { completion in
            now = 20
            completion("visible")
        }

        XCTAssertNil(visibility)
        XCTAssertTrue(request.completed)
    }

    func testDocumentVisibilityRequestIgnoresRepeatedCallbackAfterSuccess() async {
        let request = SmokeDocumentVisibilityRequest()
        let visibility = await request.wait(timeoutNanoseconds: 1_000_000_000) { completion in
            completion("visible")
            completion("hidden")
        }

        XCTAssertEqual(visibility, "visible")
        XCTAssertTrue(request.completed)
    }
}
