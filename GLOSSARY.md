# My Software Factory Manager

The operational hub of a software consultancy: one place to manage clients, their projects and their software development requests, from first contact to delivery and maintenance.

## Language

### The consultancy

**Consultancy**:
The software company the system is run for. There is exactly one, and every client, project and user belongs to it.
_Avoid_: Tenant, organization, company, account

**User**:
A person registered in the system, who logs in with their e-mail. An administrator registers a user and gives them at least one role. A deactivated user cannot log in and stays in the records; no user is removed.
_Avoid_: Account, member, login

**Role**:
A named set of scopes given to users. A user can have several roles and may do what any of them allows.
_Avoid_: Permission group, profile, group

**Administrator**:
A user who has the administrator role. That role always grants every scope, so an administrator may register other users, change the data of the consultancy and see how the system itself is running, starting with the health report. The role cannot be changed or removed. The first administrator is created in the setup.
_Avoid_: Admin, super admin, owner, manager

**Developer**:
A user of the consultancy who works on its projects. The developer role is created in the setup and can be changed like any other role.
_Avoid_: Dev, programmer, member

**Setup**:
The one-time registration of the consultancy and of its first administrator, done before anyone can log in. Both are created together when it is concluded.
_Avoid_: Onboarding, installation, sign-up, first login

**Setup code**:
A code that opens the setup. The installer issues it on the server, and it stops working when the setup is concluded. A new one, issued when no administrator can log in, opens the setup again to register an administrator. It is not a user.
_Avoid_: Super admin, root user, initial password, bootstrap token

**Installer**:
Whoever installs the system and runs its commands on the server, acting as no user. The audit log names the installer as the one who issued a setup code.
_Avoid_: Operator, system administrator, root, admin

**Seed**:
The command that fills an empty database with a fictitious consultancy and its users, through the use cases of the setup, the same on every run, so that development and staging open on the login screen. Production never runs it.
_Avoid_: Fixtures, sample data, demo data, bootstrap

**Tax ID**:
The identifier a country's tax authority gives a company, such as the CNPJ in Brazil. It is read together with the country of the company.
_Avoid_: CNPJ, VAT number, company number

**National ID**:
The identifier a country gives a person, such as the CPF in Brazil. It is read together with the country of the person.
_Avoid_: CPF, SSN, document number, personal ID

### Credentials and access

**Credential**:
An access the consultancy holds to a system of a client, such as a login and password, an API key or a server. It always belongs to a client or to one of that client's projects.
_Avoid_: Secret, password, login

**Access token**:
What a user creates so that an agent can enter the system and act on their behalf. It has a name and a set of scopes chosen when it is created, and it can be revoked. An action taken with it is recorded as that user's, together with the access token used.
_Avoid_: API key, personal access token

**Login code**:
A six-digit code the system sends to a user's e-mail so that they can log in without a login provider. It works once, for a short time, and stops working after three wrong attempts.
_Avoid_: Token, OTP, PIN, verification code

**Login code request**:
The record that a login code was asked for a user, from which the code is computed whenever it is needed; the code itself is kept nowhere. A new one ends the one before.
_Avoid_: OTP request, code record, challenge

**Login provider**:
An outside service through which a user proves who they are in order to log in, such as Google, Microsoft or GitHub.
_Avoid_: Identity provider, SSO, social login, external provider

**Local login provider**:
A login provider that exists only in development and tests, turned on by configuration and backed by an identity container with a fixed list of people. Production and staging never have one.
_Avoid_: Dex, mock provider, dev login, fake login

**Provider link**:
The record that a user is the holder of one account at a login provider, kept from the first login through it. A user can have several, one per provider.
_Avoid_: Linked account, identity, connection

**Session**:
A user's presence in the system from a login to a logout, kept by the system and known to the browser only by an opaque cookie. It ends after 7 days without use, 30 days after the login, or when the user is deactivated.
_Avoid_: Token, auth state, logged-in state

**Scope**:
What a role or an access token allows: read or write in one area of the system. Reading the value of a credential is a scope of its own. An access token never allows more than the roles of its user do.
_Avoid_: Permission

**Audit log**:
The permanent record of sensitive operations: reading the value of a credential, logging in, creating or revoking an access token, issuing a setup code, concluding a setup, changing the roles of a user, and deactivating or reactivating a user. Nothing in it is edited or deleted. It is not the history of work that screens show.
_Avoid_: Activity history, history, log

### System health

**System health**:
Whether the system can do its work right now: operational, degraded or down. It is degraded when an optional part is down, and down when an essential part is.
_Avoid_: Status, uptime, availability

**Part**:
Something the system needs in order to work, such as its database, its file storage or an LLM provider. An essential part takes the whole system down with it; an optional part takes only the features that use it. A part is operational, down or not configured.
_Avoid_: Component, dependency, service

**Health report**:
The state of every part at the moment it is asked for, together with the version of the system that is running. It keeps no history.
_Avoid_: Status page, health check, diagnostics

### Languages and translation

**Supported language**:
A language the system offers for its interface, its messages and its configuration content.
_Avoid_: Locale, idiom

**Default language**:
The supported language used when a person's own language is not supported or a version is missing. It is English.
_Avoid_: Fallback language, base language

**Preferred language**:
The supported language a user has chosen to read the system in.
_Avoid_: User locale, display language

**Configuration content**:
Content registered once and reused by everyone, such as templates and the names of categories and statuses. It has one version per supported language.
_Avoid_: Master data, reference data, settings

**Free text**:
Text a person writes in the course of work, such as a description, a comment or a message. It exists only in its original language.
_Avoid_: User content, user-generated content

**Original language**:
The language in which a piece of configuration content or free text was first written.
_Avoid_: Source language, native language

**Automatic draft**:
A version of configuration content proposed by the system and not yet reviewed by a person.
_Avoid_: Machine translation, suggestion

**On-demand translation**:
An automatic rendering of free text into the reader's language, shown at their request. It never replaces the original.
_Avoid_: Auto-translation, translated version

### Environments

**Staging**:
The environment where the maintainer tries one pull request before merging it, on a server of its own that holds only the seed's data. Turning it on creates that server and turning it off destroys it, both by hand, so it costs only while it is on.
_Avoid_: Preview, homologation, QA environment, test server, sandbox

**Publish**:
To put the code of one commit on a server and restart the system there. A pull request with the staging label is published to staging, whose data starts again from the seed; a merge into main is published to production.
_Avoid_: Deploy, ship, release, upload, provision
