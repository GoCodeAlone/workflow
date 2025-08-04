Feature: Workflow UI Authentication
  As a user
  I want to authenticate with the workflow UI
  So that I can manage my workflows securely

  Background:
    Given the workflow UI is running
    And there is a default tenant "default"
    And there is an admin user "admin" with password "admin"

  Scenario: Successful login
    When I login with username "admin" and password "admin"
    Then I should receive a valid JWT token
    And I should see my user information
    And I should see my tenant information

  Scenario: Failed login with invalid credentials
    When I login with username "admin" and password "wrong"
    Then I should receive an authentication error
    And I should not receive a token

  Scenario: Access protected endpoint without token
    When I try to access "/api/workflows" without a token
    Then I should receive an unauthorized error

  Scenario: Access protected endpoint with valid token
    Given I am logged in as "admin"
    When I access "/api/workflows" with my token
    Then I should receive a successful response