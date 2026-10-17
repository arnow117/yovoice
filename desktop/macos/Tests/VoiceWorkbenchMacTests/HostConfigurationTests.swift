import XCTest
@testable import VoiceWorkbenchMac

final class HostConfigurationTests: XCTestCase {
    func testStandardBundleUsesDefaultRootAndPreparation() {
        let home = URL(fileURLWithPath: "/tmp/yovoice-home", isDirectory: true)
        let configuration = HostConfiguration(environment: [:], bundleInfo: [:], homeDirectory: home)

        XCTAssertEqual(configuration.root.path, "/tmp/yovoice-home/.yovoice")
        XCTAssertTrue(configuration.shouldPrepareData)
    }

    func testExplicitDataRootAndRuntimeEnvironmentTakePriority() {
        let configuration = HostConfiguration(
            environment: [
                "WORKBENCH_DATA": "/tmp/speaker-data",
                "YOVOICE_SPEAKER_MODEL": "",
            ],
            bundleInfo: [
                "YovoiceSpeakerValidation": true,
                "YovoiceValidationDataDirectory": "/tmp/ignored-validation-data",
                "YovoiceSpeakerRuntime": [
                    "MODEL": "/bundle/model.onnx",
                    "RUNNER": "/bundle/runner.py",
                ],
            ],
            homeDirectory: URL(fileURLWithPath: "/tmp/home", isDirectory: true),
        )

        XCTAssertEqual(configuration.root.path, "/tmp/speaker-data")
        XCTAssertFalse(configuration.shouldPrepareData)
        XCTAssertEqual(configuration.speakerEnvironment["YOVOICE_SPEAKER_MODEL"], "")
        XCTAssertEqual(configuration.speakerEnvironment["YOVOICE_SPEAKER_RUNNER"], "/bundle/runner.py")
    }

    func testValidationBundleUsesItsPrivateRootWithoutMigration() {
        let configuration = HostConfiguration(
            environment: [:],
            bundleInfo: [
                "YovoiceSpeakerValidation": true,
                "YovoiceValidationDataDirectory": "/tmp/validation-data",
                "YovoiceSpeakerRuntime": ["PYTHON": "/bundle/python"],
            ],
            homeDirectory: URL(fileURLWithPath: "/tmp/home", isDirectory: true),
        )

        XCTAssertEqual(configuration.root.path, "/tmp/validation-data")
        XCTAssertFalse(configuration.shouldPrepareData)
        XCTAssertEqual(configuration.speakerEnvironment["YOVOICE_SPEAKER_PYTHON"], "/bundle/python")
    }

    func testOnlySpeakerMethodsUseExtendedTransportTimeout() {
        XCTAssertEqual(HostConfiguration.requestTimeout(for: "speaker.enroll"), 660)
        XCTAssertEqual(HostConfiguration.requestTimeout(for: "speaker.identify"), 660)
        XCTAssertEqual(HostConfiguration.requestTimeout(for: "speaker.generate"), 660)
        XCTAssertEqual(HostConfiguration.requestTimeout(for: "speaker.cancel"), 120)
        XCTAssertEqual(HostConfiguration.requestTimeout(for: "generation.start"), 120)
    }
}
